package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/raden-solutions/net-xms/pkg/models"
)

// Make sure Datasource implements required interfaces. This is important to do
// since otherwise we will only get a not implemented error response from plugin in
// runtime. In this example datasource instance implements backend.QueryDataHandler,
// backend.CheckHealthHandler interfaces. Plugin should not implement all these
// interfaces - only those which are required for a particular task.
var (
	_ backend.QueryDataHandler      = (*NetXMSDatasource)(nil)
	_ backend.CheckHealthHandler    = (*NetXMSDatasource)(nil)
	_ backend.CallResourceHandler   = (*NetXMSDatasource)(nil)
	_ instancemgmt.InstanceDisposer = (*NetXMSDatasource)(nil)
)

// NetXMSDatasource is datasource which can respond to data queries, reports
// its health.
type NetXMSDatasource struct {
	client          *netxmsClient
	queryHandler    backend.QueryDataHandler
	resourceHandler backend.CallResourceHandler
}

type netxmsClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// upstreamError is a non-200 response from the NetXMS server.
type upstreamError struct {
	status int
	body   []byte
}

func (e *upstreamError) Error() string {
	return httpStatusErrorMessage(e.status)
}

// do sends a request to the NetXMS server and returns the body of a 200 response. A nil body
// sends no body and no Content-Type; anything else is sent as JSON.
func (c *netxmsClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader = http.NoBody
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(bodyBytes)
	}

	request, err := http.NewRequestWithContext(ctx, method, joinURL(c.baseURL, path), reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)

	result, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer result.Body.Close()

	respBody, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if result.StatusCode != http.StatusOK {
		return nil, &upstreamError{status: result.StatusCode, body: respBody}
	}
	return respBody, nil
}

func queryError(err error) backend.DataResponse {
	upstream, ok := errors.AsType[*upstreamError](err)
	if !ok {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}
	if upstream.status == http.StatusUnauthorized {
		return backend.ErrDataResponse(backend.StatusUnauthorized, "Unauthorized: Invalid API key")
	}
	return parseErrorResponse(upstream.status, upstream.body)
}

// NewDatasource creates a new NetXMS datasource instance
func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	config, err := models.LoadPluginSettings(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugin settings: %w", err)
	}
	ds := &NetXMSDatasource{
		client: &netxmsClient{
			baseURL: config.ServerAddress,
			apiKey:  config.Secrets.ApiKey,
			http:    &http.Client{Timeout: 10 * time.Second},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/alarmObjects", ds.handleAlarmObjects)
	mux.HandleFunc("/dciObjects", ds.handleDciObjects)
	mux.HandleFunc("/objectQueries", ds.handleObjectQueries)
	mux.HandleFunc("/objectQueryObjects", ds.handleObjectQueryObjects)
	mux.HandleFunc("/summaryTableObjects", ds.handleSummaryTableObjects)
	mux.HandleFunc("/summaryTables", ds.handleSummaryTables)
	mux.HandleFunc("/dcis", ds.handleDciList)
	ds.resourceHandler = httpadapter.New(mux)
	queryTypeMux := datasource.NewQueryTypeMux()
	queryTypeMux.HandleFunc("alarms", ds.handleAlarmQuery)
	queryTypeMux.HandleFunc("dciValues", ds.handleDciValues)
	queryTypeMux.HandleFunc("summaryTables", ds.handleSummaryTableQuery)
	queryTypeMux.HandleFunc("objectQueries", ds.handleObjectQueryQuery)
	queryTypeMux.HandleFunc("objectStatus", ds.handleObjectStatusQuery)
	ds.queryHandler = queryTypeMux
	return ds, nil
}

// Dispose here tells plugin SDK that plugin wants to clean up resources when a new instance
// created. As soon as datasource settings change detected by SDK old datasource instance will
// be disposed and a new one will be created using NewSampleDatasource factory function.
func (d *NetXMSDatasource) Dispose() {}

type queryModel struct {
	SourceObjectId string `json:"sourceObjectId"`
	DciId          string `json:"dciId"`
}

type alarmResponse struct {
	Id         uint32    `json:"Id"`
	Severity   string    `json:"Severity"`
	State      string    `json:"State"`
	Source     string    `json:"Source"`
	Message    string    `json:"Message"`
	Count      uint32    `json:"Count"`
	AckBy      string    `json:"Ack/Resolve by"`
	Created    time.Time `json:"Created"`
	LastChange time.Time `json:"Last Change"`
}

type dciValueResponse struct {
	Description string `json:"description"`
	UnitName    string `json:"unitName"`
	TierServed  string `json:"tierServed"`
	Aggregated  bool   `json:"aggregated"`
	Values      []struct {
		Timestamp string   `json:"timestamp"`
		Value     string   `json:"value"`
		Avg       *float64 `json:"avg"`
	} `json:"values"`
}

// maxDciDataRecords mirrors the server's MAX_DCI_DATA_RECORDS row limit for history requests.
const maxDciDataRecords = 200000

type dciPoint struct {
	ts    time.Time
	value string
	avg   float64
}

type requiredField struct {
	field   string
	message string
}

type tableQueryConfig struct {
	url        string
	frameName  string
	required   []requiredField
	formatBody func(map[string]any) (map[string]any, error)
}

func (d *NetXMSDatasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	resp, err := d.queryHandler.QueryData(ctx, req)
	if err != nil {
		return resp, fmt.Errorf("query data: %w", err)
	}
	return resp, nil
}

// QueryData handles multiple queries and returns multiple responses.
// req contains the queries []DataQuery (where each query contains RefID as a unique identifier).
// The QueryDataResponse contains a map of RefID to the response for each query, and each response
// contains Frames ([]*Frame).
func (d *NetXMSDatasource) handleAlarmQuery(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()

	for _, q := range req.Queries {
		var qm queryModel
		err := json.Unmarshal(q.JSON, &qm)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err.Error()))
			continue
		}

		res := d.query(ctx, qm.SourceObjectId)
		response.Responses[q.RefID] = res
	}

	return response, nil
}

func (d *NetXMSDatasource) query(ctx context.Context, rootObjectId string) backend.DataResponse {
	var response backend.DataResponse

	reqBody := map[string]any{}
	if rootObjectId != "" {
		rootObjectIdNum, parseErr := strconv.ParseInt(rootObjectId, 10, 64)
		if parseErr != nil {
			return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("invalid rootObjectId: %v", parseErr.Error()))
		}
		reqBody["rootObjectId"] = rootObjectIdNum
	}

	body, err := d.client.do(ctx, http.MethodPost, "v1/grafana/infinity/alarms", reqBody)
	if err != nil {
		return queryError(err)
	}

	var alarms []alarmResponse
	if err := json.Unmarshal(body, &alarms); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err.Error()))
	}

	frame := data.NewFrame("alarms")

	ids := make([]uint32, len(alarms))
	severities := make([]string, len(alarms))
	states := make([]string, len(alarms))
	sources := make([]string, len(alarms))
	messages := make([]string, len(alarms))
	counts := make([]uint32, len(alarms))
	ackBy := make([]string, len(alarms))
	created := make([]time.Time, len(alarms))
	lastChange := make([]time.Time, len(alarms))

	for i, alarm := range alarms {
		ids[i] = alarm.Id
		severities[i] = alarm.Severity
		states[i] = alarm.State
		sources[i] = alarm.Source
		messages[i] = alarm.Message
		counts[i] = alarm.Count
		ackBy[i] = alarm.AckBy
		created[i] = alarm.Created
		lastChange[i] = alarm.LastChange
	}

	severityField := data.NewField("Severity", nil, severities)
	severityMapping := make(data.ValueMapper, len(severityColors))
	for _, severity := range severityColors {
		severityMapping[severity.name] = data.ValueMappingResult{Text: severity.name, Color: severity.color}
	}
	severityField.Config = &data.FieldConfig{
		Mappings: data.ValueMappings{severityMapping},
	}
	stateField := data.NewField("State", nil, states)
	stateField.Config = &data.FieldConfig{
		Mappings: data.ValueMappings{
			data.ValueMapper{
				"Outstanding":  {Text: "Outstanding", Color: "yellow"},
				"Acknowledged": {Text: "Acknowledged", Color: "greenyellow"},
				"Resolved":     {Text: "Resolved", Color: "green"},
			},
		},
	}

	frame.Fields = append(frame.Fields,
		data.NewField("Id", nil, ids),
		severityField,
		stateField,
		data.NewField("Source", nil, sources),
		data.NewField("Message", nil, messages),
		data.NewField("Count", nil, counts),
		data.NewField("Ack/Resolve by", nil, ackBy),
		data.NewField("Created", nil, created),
		data.NewField("Last Change", nil, lastChange),
	)

	response.Frames = append(response.Frames, frame)
	return response
}

// versionAtLeast compares numeric version parts, treating missing parts as zero and ignoring suffixes.
func versionAtLeast(actualVersion, requireVersion string) bool {
	leadingNumber := func(part string) int {
		end := 0
		for end < len(part) && part[end] >= '0' && part[end] <= '9' {
			end++
		}
		number, _ := strconv.Atoi(part[:end])
		return number
	}
	actualVersionParts := strings.Split(actualVersion, ".")
	requiredVersionParts := strings.Split(requireVersion, ".")
	maxLen := max(len(actualVersionParts), len(requiredVersionParts))
	for i := range maxLen {
		var actualVersionNum, requiredVersionNum int
		if i < len(actualVersionParts) {
			actualVersionNum = leadingNumber(actualVersionParts[i])
		}
		if i < len(requiredVersionParts) {
			requiredVersionNum = leadingNumber(requiredVersionParts[i])
		}
		if actualVersionNum > requiredVersionNum {
			return true
		}
		if actualVersionNum < requiredVersionNum {
			return false
		}
	}
	return true
}

// CheckHealth handles health checks sent from Grafana to the plugin.
// The main use case for these health checks is the test button on the
// datasource configuration page which allows users to verify that
// a datasource is working as expected.
func (d *NetXMSDatasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	res := &backend.CheckHealthResult{}

	if d.client.apiKey == "" {
		res.Status = backend.HealthStatusError
		res.Message = "API key is missing"
		return res, nil
	}

	if d.client.baseURL == "" {
		res.Status = backend.HealthStatusError
		res.Message = "Server address is missing"
		return res, nil
	}

	body, err := d.client.do(ctx, http.MethodGet, "v1/server-info", nil)
	if err != nil {
		res.Status = backend.HealthStatusError
		if upstream, ok := errors.AsType[*upstreamError](err); ok {
			res.Message = fmt.Sprintf("Server returned status code: %d (%d %s)", upstream.status, upstream.status, http.StatusText(upstream.status))
		} else {
			res.Message = err.Error()
		}
		return res, nil
	}

	var serverInfo map[string]any
	if err := json.Unmarshal(body, &serverInfo); err != nil {
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("Failed to parse server response: %v", err)
		return res, nil
	}
	actualVersion, ok := serverInfo["version"].(string)
	if !ok {
		res.Status = backend.HealthStatusError
		res.Message = "Server response missing version field"
		return res, nil
	}
	requiredVersion := "7.0.0"
	if !versionAtLeast(actualVersion, requiredVersion) {
		log.DefaultLogger.Warn("Server version is below required minimum", "actual", actualVersion, "required", requiredVersion)
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("Server version (current: %s) should be equal or greater than %s", actualVersion, requiredVersion)
		return res, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Data source is working",
	}, nil
}

func (ds *NetXMSDatasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if err := ds.resourceHandler.CallResource(ctx, req, sender); err != nil {
		return fmt.Errorf("call resource: %w", err)
	}
	return nil
}

// This method handles all request to get lists of items in format name : id
func (ds *NetXMSDatasource) handleQuery(url string, rw http.ResponseWriter, req *http.Request) {
	body, err := ds.client.do(req.Context(), http.MethodGet, url, nil)
	if upstream, ok := errors.AsType[*upstreamError](err); ok {
		writeResourceError(rw, upstream.status, upstream.body)
		return
	}
	if err != nil {
		log.DefaultLogger.Error("NetXMS request failed", "path", url, "error", err)
		writeJSONError(rw, http.StatusBadGateway, err.Error())
		return
	}

	// Parse JSON and sort by label
	var responseData map[string]any
	if unmarshalErr := json.Unmarshal(body, &responseData); unmarshalErr != nil {
		writeJSONResponse(rw, body)
		return
	}

	// Check if "objects" field exists and is an array
	objects, ok := responseData["objects"].([]any)
	if !ok {
		writeJSONResponse(rw, body)
		return
	}

	// Convert to slice of maps for sorting
	jsonData := make([]map[string]any, len(objects))
	for i, obj := range objects {
		objMap, ok := obj.(map[string]any)
		if !ok {
			writeJSONResponse(rw, body)
			return
		}
		jsonData[i] = objMap
	}

	// Sort by name field
	sort.Slice(jsonData, func(i, j int) bool {
		nameI, okI := jsonData[i]["name"].(string)
		nameJ, okJ := jsonData[j]["name"].(string)
		if !okI || !okJ {
			return false
		}
		return nameI < nameJ
	})

	// Update the objects field with sorted data
	responseData["objects"] = jsonData

	// Marshal back to JSON
	sortedBody, err := json.Marshal(responseData)
	if err != nil {
		writeJSONError(rw, http.StatusInternalServerError, fmt.Sprintf("failed to marshal sorted response: %v", err))
		return
	}

	writeJSONResponse(rw, sortedBody)
}

func (ds *NetXMSDatasource) handleAlarmObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/object-list?filter=alarm", rw, req)
}

func (ds *NetXMSDatasource) handleDciObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/object-list?filter=dci", rw, req)
}

func (ds *NetXMSDatasource) handleSummaryTables(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/summary-table-list", rw, req)
}

func (ds *NetXMSDatasource) handleSummaryTableObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/object-list?filter=summary", rw, req)
}

func (ds *NetXMSDatasource) handleObjectQueries(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/query-list", rw, req)
}

func (ds *NetXMSDatasource) handleObjectQueryObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/object-list?filter=query", rw, req)
}

func (ds *NetXMSDatasource) handleDciList(rw http.ResponseWriter, req *http.Request) {
	objectID := req.URL.Query().Get("objectId")
	if objectID == "" {
		writeJSONError(rw, http.StatusBadRequest, "missing objectId parameter")
		return
	}
	if _, err := strconv.ParseInt(objectID, 10, 64); err != nil {
		writeJSONError(rw, http.StatusBadRequest, "objectId must be numeric")
		return
	}
	path := fmt.Sprintf("/v1/grafana/objects/%s/dci-list", objectID)
	ds.handleQuery(path, rw, req)
}

func (ds *NetXMSDatasource) handleDciValues(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		var qm queryModel
		if err := json.Unmarshal(q.JSON, &qm); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err.Error()))
			continue
		}

		if _, err := strconv.ParseInt(qm.SourceObjectId, 10, 64); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, "sourceObjectId must be numeric")
			continue
		}
		if _, err := strconv.ParseInt(qm.DciId, 10, 64); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, "dciId must be numeric")
			continue
		}

		timeFrom := q.TimeRange.From.Unix()
		timeTo := q.TimeRange.To.Unix()

		path := fmt.Sprintf("v1/objects/%s/data-collection/%s/history?timeFrom=%d&timeTo=%d",
			qm.SourceObjectId, qm.DciId, timeFrom, timeTo)
		if q.MaxDataPoints > 0 {
			path += fmt.Sprintf("&maxDataPoints=%d", q.MaxDataPoints)
		}

		body, err := ds.client.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			response.Responses[q.RefID] = queryError(err)
			continue
		}

		var dciData dciValueResponse
		if err = json.Unmarshal(body, &dciData); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err))
			continue
		}

		frame, err := dciFrame(&dciData)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
			continue
		}

		response.Responses[q.RefID] = backend.DataResponse{
			Frames: data.Frames{frame},
		}
	}
	return response, nil
}

// dciFrame converts a history response into a time/value frame. Aggregated responses
// (on-the-fly buckets or hourly/daily tiers) carry avg instead of value.
func dciFrame(dciData *dciValueResponse) (*data.Frame, error) {
	frame := data.NewFrame(dciData.Description)

	points := make([]dciPoint, 0, len(dciData.Values))
	for _, v := range dciData.Values {
		// The server emits null for a zero timestamp; skip such points instead of failing the series.
		if v.Timestamp == "" {
			continue
		}
		if dciData.Aggregated && v.Avg == nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, v.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp %q: %w", v.Timestamp, err)
		}
		p := dciPoint{ts: t, value: v.Value}
		if v.Avg != nil {
			p.avg = *v.Avg
		}
		points = append(points, p)
	}

	slices.SortStableFunc(points, func(a, b dciPoint) int { return a.ts.Compare(b.ts) })

	times := make([]time.Time, 0, len(points))
	stringValues := make([]string, 0, len(points))
	floatValues := make([]float64, 0, len(points))
	isNumeric := true

	for _, p := range points {
		times = append(times, p.ts)
		if dciData.Aggregated {
			floatValues = append(floatValues, p.avg)
			continue
		}
		stringValues = append(stringValues, p.value)

		if isNumeric {
			val, err := strconv.ParseFloat(p.value, 64)
			if err != nil {
				isNumeric = false
			} else {
				floatValues = append(floatValues, val)
			}
		}
	}

	timeField := data.NewField("time", nil, times)
	if interval := dciTierInterval(dciData.TierServed); interval > 0 {
		timeField.Config = &data.FieldConfig{Interval: float64(interval.Milliseconds())}
	}

	if isNumeric {
		frame.Fields = append(frame.Fields, timeField, data.NewField("value", map[string]string{"unit": dciData.UnitName}, floatValues))
	} else {
		frame.Fields = append(frame.Fields, timeField, data.NewField("value", nil, stringValues))
	}

	// Bucketed raw reads have no row limit; raw rows and tier rows are cut to the newest maxDciDataRecords.
	if len(dciData.Values) >= maxDciDataRecords && (!dciData.Aggregated || dciData.TierServed != "raw") {
		frame.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityWarning,
			Text:     fmt.Sprintf("The server returned its %d-row limit; earlier values may be missing. Narrow the time range.", maxDciDataRecords),
		})
	}

	return frame, nil
}

// dciTierInterval returns the step between hourly/daily aggregate rows. Raw buckets get none: they can be
// narrower than the polling interval, and Grafana would insert nulls between every pair of samples.
func dciTierInterval(tierServed string) time.Duration {
	switch tierServed {
	case "hourly":
		return time.Hour
	case "daily":
		return 24 * time.Hour
	default:
		return 0
	}
}

// decodeRow decodes a JSON object into a map and returns its keys in document order.
func decodeRow(raw []byte) ([]string, map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return nil, nil, fmt.Errorf("expected object, got %v", token)
	}

	var keys []string
	row := make(map[string]any)
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("read key token: %w", err)
		}
		keyStr, ok := key.(string)
		if !ok {
			return nil, nil, fmt.Errorf("expected string key, got %v", key)
		}

		var value any
		if err := dec.Decode(&value); err != nil {
			return nil, nil, fmt.Errorf("decode value for key %q: %w", keyStr, err)
		}

		keys = append(keys, keyStr)
		row[keyStr] = value
	}

	if _, err := dec.Token(); err != nil {
		return nil, nil, fmt.Errorf("read closing token: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("unexpected data after object")
	}

	return keys, row, nil
}

// decodeTableRows decodes each row into a map and returns the union of row keys in first-seen order,
// since object query rows carry only the fields set for that object.
func decodeTableRows(rawRows []json.RawMessage) ([]string, []map[string]any, error) {
	var columns []string
	seen := make(map[string]bool)
	rows := make([]map[string]any, 0, len(rawRows))
	for i, raw := range rawRows {
		keys, row, err := decodeRow(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("row %d: %w", i, err)
		}
		for _, key := range keys {
			if !seen[key] {
				seen[key] = true
				columns = append(columns, key)
			}
		}
		rows = append(rows, row)
	}
	return columns, rows, nil
}

func (d *NetXMSDatasource) handleTableQuery(ctx context.Context, req *backend.QueryDataRequest, queryConfig tableQueryConfig) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()

	for _, q := range req.Queries {
		var qm map[string]any
		if err := json.Unmarshal(q.JSON, &qm); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err.Error()))
			continue
		}

		valid := true
		for _, rf := range queryConfig.required {
			if value, ok := qm[rf.field].(string); !ok || value == "" {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, rf.message)
				valid = false
				break
			}
		}
		if !valid {
			continue
		}

		reqBody, err := queryConfig.formatBody(qm)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to format request body: %v", err))
			continue
		}

		body, err := d.client.do(ctx, http.MethodPost, queryConfig.url, reqBody)
		if err != nil {
			response.Responses[q.RefID] = queryError(err)
			continue
		}

		var rawRows []json.RawMessage
		if err = json.Unmarshal(body, &rawRows); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err))
			continue
		}

		columns, rows, err := decodeTableRows(rawRows)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err))
			continue
		}

		frame := data.NewFrame(queryConfig.frameName)
		for _, columnName := range columns {
			frame.Fields = append(frame.Fields, buildTableColumn(columnName, rows))
		}

		response.Responses[q.RefID] = backend.DataResponse{
			Frames: data.Frames{frame},
		}
	}

	return response, nil
}

func (d *NetXMSDatasource) handleSummaryTableQuery(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	return d.handleTableQuery(ctx, req, tableQueryConfig{
		url:       "/v1/grafana/infinity/summary-table",
		frameName: "summary-table",
		required: []requiredField{
			{"summaryTableId", "summaryTableId is required"},
		},
		formatBody: func(qm map[string]any) (map[string]any, error) {
			reqBody := make(map[string]any)

			if rootObjectId, ok := qm["sourceObjectId"].(string); ok && rootObjectId != "" {
				rootObjectIdNum, err := strconv.ParseInt(rootObjectId, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid rootObjectId: %w", err)
				}
				reqBody["rootObjectId"] = rootObjectIdNum
			}

			if tableId, ok := qm["summaryTableId"].(string); ok && tableId != "" {
				tableIdNum, err := strconv.ParseInt(tableId, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid tableId: %w", err)
				}
				reqBody["tableId"] = tableIdNum
			}

			return reqBody, nil
		},
	})
}

func (d *NetXMSDatasource) handleObjectQueryQuery(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	return d.handleTableQuery(ctx, req, tableQueryConfig{
		url:       "/v1/grafana/infinity/object-query",
		frameName: "object-query",
		required: []requiredField{
			{"objectQueryId", "objectQueryId is required"},
		},
		formatBody: func(qm map[string]any) (map[string]any, error) {
			reqBody := make(map[string]any)

			if rootObjectId, ok := qm["sourceObjectId"].(string); ok && rootObjectId != "" {
				rootObjectIdNum, err := strconv.ParseInt(rootObjectId, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid rootObjectId: %w", err)
				}
				reqBody["rootObjectId"] = rootObjectIdNum
			}

			if queryId, ok := qm["objectQueryId"].(string); ok && queryId != "" {
				queryIdNum, err := strconv.ParseInt(queryId, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid queryId: %w", err)
				}
				reqBody["queryId"] = queryIdNum
			}

			if raw, ok := qm["queryParameters"].(string); ok && strings.TrimSpace(raw) != "" {
				inputFields, err := parseInputFields(raw)
				if err != nil {
					return nil, err
				}
				reqBody["inputFields"] = inputFields
			}

			return reqBody, nil
		},
	})
}

type objectStatusResponse struct {
	Name   string `json:"Name"`
	Status int32  `json:"Status"`
}

// severityColors follows the numeric object status order used by NetXMS.
var severityColors = [...]struct{ name, color string }{
	{"Normal", "rgb(0, 137, 0)"},
	{"Warning", "rgb(0, 142, 145)"},
	{"Minor", "rgb(201, 198, 0)"},
	{"Major", "rgb(223, 102, 0)"},
	{"Critical", "rgb(160, 0, 0)"},
	{"Unknown", "rgb(33, 33, 248)"},
	{"Unmanaged", "rgb(113, 113, 113)"},
	{"Disabled", "rgb(100, 41, 0)"},
	{"Testing", "rgb(138, 0, 143)"},
}

func objectStatusColor(status int32) string {
	if status >= 0 && int(status) < len(severityColors) {
		return severityColors[status].color
	}
	return "rgb(128, 128, 128)"
}

func (d *NetXMSDatasource) handleObjectStatusQuery(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()

	for _, q := range req.Queries {
		var qm queryModel
		if err := json.Unmarshal(q.JSON, &qm); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err.Error()))
			continue
		}

		reqBody := map[string]any{}
		if qm.SourceObjectId != "" {
			rootObjectIdNum, parseErr := strconv.ParseInt(qm.SourceObjectId, 10, 64)
			if parseErr != nil {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, "sourceObjectId must be numeric")
				continue
			}
			reqBody["rootObjectId"] = rootObjectIdNum
		}

		body, err := d.client.do(ctx, http.MethodPost, "/v1/grafana/objects-status", reqBody)
		if err != nil {
			response.Responses[q.RefID] = queryError(err)
			continue
		}

		var statusData []objectStatusResponse
		if err := json.Unmarshal(body, &statusData); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err))
			continue
		}

		frames := make(data.Frames, 0, len(statusData))
		for _, obj := range statusData {
			frame := data.NewFrame(obj.Name)

			// Use DisplayName to show object name in stat panel
			nameField := data.NewField("Name", nil, []string{obj.Name})
			nameField.Config = &data.FieldConfig{
				Mappings: data.ValueMappings{
					data.ValueMapper{
						obj.Name: {Text: obj.Name, Color: objectStatusColor(obj.Status)},
					},
				},
			}
			frame.Fields = append(frame.Fields, nameField)
			frames = append(frames, frame)
		}

		response.Responses[q.RefID] = backend.DataResponse{
			Frames: frames,
		}
	}

	return response, nil
}

// buildTableColumn builds a frame field for one column of a table response. Numeric and
// boolean columns keep their type (nullable) when every non-null cell has it; anything else,
// including mixed columns, becomes text with empty strings for nulls.
func buildTableColumn(name string, rows []map[string]any) *data.Field {
	hasValue, allFloat, allBool := false, true, true
	for _, row := range rows {
		v := row[name]
		if v == nil {
			continue
		}
		hasValue = true
		if _, ok := v.(float64); !ok {
			allFloat = false
		}
		if _, ok := v.(bool); !ok {
			allBool = false
		}
	}

	switch {
	case hasValue && allFloat:
		values := make([]*float64, len(rows))
		for i, row := range rows {
			if f, ok := row[name].(float64); ok {
				values[i] = &f
			}
		}
		return data.NewField(name, nil, values)
	case hasValue && allBool:
		values := make([]*bool, len(rows))
		for i, row := range rows {
			if b, ok := row[name].(bool); ok {
				values[i] = &b
			}
		}
		return data.NewField(name, nil, values)
	default:
		values := make([]string, len(rows))
		for i, row := range rows {
			if v := row[name]; v != nil {
				values[i] = fmt.Sprintf("%v", v)
			}
		}
		return data.NewField(name, nil, values)
	}
}

// parseInputFields converts the query parameters JSON entered in the editor into the flat
// object the NetXMS "inputFields" request field expects. Accepts an object ({"name": "value"}),
// an array of such objects, or an array of {"key": ..., "value": ...} pairs.
func parseInputFields(raw string) (map[string]any, error) {
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("invalid queryParameters JSON: %w", err)
	}

	fields := make(map[string]any)
	switch v := decoded.(type) {
	case map[string]any:
		maps.Copy(fields, v)
	case []any:
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("invalid queryParameters: array items must be objects")
			}
			if key, isPair := obj["key"].(string); isPair && len(obj) == 2 {
				if value, exists := obj["value"]; exists {
					fields[key] = value
					continue
				}
			}
			maps.Copy(fields, obj)
		}
	default:
		return nil, errors.New("invalid queryParameters: expected a JSON object or array")
	}
	return fields, nil
}

// writeResourceError reports an upstream NetXMS error to the frontend as 502 Bad Gateway with
// a JSON body carrying the reason, so the query editor can show it. The upstream status is not
// forwarded as is: a 401 from a plugin resource makes Grafana's frontend treat it as an expired
// Grafana session and start token rotation.
func writeResourceError(rw http.ResponseWriter, statusCode int, body []byte) {
	reason := httpStatusErrorMessage(statusCode)
	var reasonResp struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &reasonResp); err == nil && reasonResp.Reason != "" {
		reason = reasonResp.Reason
	} else if statusCode == http.StatusUnauthorized {
		reason = "Unauthorized: Invalid API key"
	}
	writeJSONError(rw, http.StatusBadGateway, reason)
}

// writeJSONError writes a JSON body with a reason, which the query editor shows for failed resource requests.
func writeJSONError(rw http.ResponseWriter, status int, reason string) {
	payload, _ := json.Marshal(map[string]string{"reason": reason})
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_, _ = rw.Write(payload)
}

// parseErrorResponse extracts error message from response body and returns appropriate DataResponse
func parseErrorResponse(statusCode int, body []byte) backend.DataResponse {
	var reasonResp struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &reasonResp); err == nil && reasonResp.Reason != "" {
		return backend.ErrDataResponse(httpStatusToBackendStatus(statusCode), "Request error: "+reasonResp.Reason)
	}
	return backend.ErrDataResponse(httpStatusToBackendStatus(statusCode), httpStatusErrorMessage(statusCode))
}

// httpStatusErrorMessage describes an HTTP error when NetXMS supplies no reason.
func httpStatusErrorMessage(statusCode int) string {
	message := fmt.Sprintf("Request error: %d", statusCode)
	if statusText := http.StatusText(statusCode); statusText != "" {
		message += " " + statusText
	}
	return message
}

// httpStatusToBackendStatus maps HTTP status codes to backend.Status
func httpStatusToBackendStatus(code int) backend.Status {
	if code == 400 {
		return backend.StatusBadRequest
	}
	if code == 401 {
		return backend.StatusUnauthorized
	}
	if code == 403 {
		return backend.StatusForbidden
	}
	if code == 404 {
		return backend.StatusNotFound
	}
	if code >= 500 && code < 600 {
		return backend.StatusInternal
	}
	return backend.StatusUnknown
}

func writeJSONResponse(rw http.ResponseWriter, body []byte) {
	rw.Header().Add("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	_, _ = rw.Write(body)
}

func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	path = strings.TrimLeft(path, "/")
	return base + "/" + path
}
