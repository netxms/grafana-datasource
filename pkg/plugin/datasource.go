package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
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
	queryHandler    backend.QueryDataHandler
	resourceHandler backend.CallResourceHandler
}

// Create new NetXMS datashource instance
func NewDatasource(_ context.Context, _ backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	ds := &NetXMSDatasource{}
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
	Id         int32     `json:"Id"`
	Severity   string    `json:"Severity"`
	State      string    `json:"State"`
	Source     string    `json:"Source"`
	Message    string    `json:"Message"`
	Count      int32     `json:"Count"`
	AckBy      string    `json:"Ack/Resolve by"`
	Created    time.Time `json:"Created"`
	LastChange time.Time `json:"Last Change"`
}

type dciValueResponse struct {
	Description string `json:"description"`
	UnitName    string `json:"unitName"`
	Values      []struct {
		Timestamp string `json:"timestamp"`
		Value     string `json:"value"`
	} `json:"values"`
}

type tableQueryConfig struct {
	url        string
	frameName  string
	required   map[string]string
	formatBody func(map[string]interface{}) map[string]interface{}
}

func (d *NetXMSDatasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	return d.queryHandler.QueryData(ctx, req)
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

		res := d.query(ctx, req.PluginContext, qm.SourceObjectId)
		response.Responses[q.RefID] = res
	}

	return response, nil
}

func (d *NetXMSDatasource) query(_ context.Context, pCtx backend.PluginContext, rootObjectId string) backend.DataResponse {
	var response backend.DataResponse
	config, err := models.LoadPluginSettings(*pCtx.DataSourceInstanceSettings)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to load plugin settings: %v", err))
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	statusURL := joinURL(config.ServerAddress, "v1/server-info")

	var bodyBytes []byte
	if rootObjectId != "" {
		rootObjectIdNum, err := strconv.ParseInt(rootObjectId, 10, 64)
		if err != nil {
			return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("invalid rootObjectId: %v", err.Error()))
		}
		body := map[string]interface{}{
			"rootObjectId": rootObjectIdNum,
		}
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to marshal request body: %v", err.Error()))
		}
	} else {
		bodyBytes = []byte(`{}`)
	}

	request, err := http.NewRequest("POST", statusURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to create request: %v", err.Error()))
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.Secrets.ApiKey))

	result, err := client.Do(request)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to connect to server: %v", err.Error()))
	}

	body, err := io.ReadAll(result.Body)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed read response: %v", err.Error()))
	}

	if result.StatusCode == http.StatusUnauthorized {
		return backend.ErrDataResponse(backend.StatusUnauthorized, "Unauthorized: Invalid API key")
	}

	if result.StatusCode != http.StatusOK {
		var reasonResp map[string]string
		if err := json.Unmarshal(body, &reasonResp); err == nil {
			return backend.ErrDataResponse(httpStatusToBackendStatus(result.StatusCode), fmt.Sprintf("Request error: %s", reasonResp["reason"]))
		}
		return backend.ErrDataResponse(httpStatusToBackendStatus(result.StatusCode), "Request error")
	}

	var alarms []alarmResponse
	if err := json.Unmarshal(body, &alarms); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err.Error()))
	}

	frame := data.NewFrame("alarms")

	ids := make([]int32, len(alarms))
	severities := make([]string, len(alarms))
	states := make([]string, len(alarms))
	sources := make([]string, len(alarms))
	messages := make([]string, len(alarms))
	counts := make([]int32, len(alarms))
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
	severityField.Config = &data.FieldConfig{
		Mappings: data.ValueMappings{
			data.ValueMapper{
				"Normal":    {Text: "Normal", Color: "rgb(0, 137, 0)"},
				"Warning":   {Text: "Warning", Color: "rgb(0, 142, 145)"},
				"Minor":     {Text: "Minor", Color: "rgb(201, 198, 0)"},
				"Major":     {Text: "Major", Color: "rgb(223, 102, 0)"},
				"Critical":  {Text: "Critical", Color: "rgb(160, 0, 0)"},
				"Unknown":   {Text: "Unknown", Color: "rgb(33, 33, 248)"},
				"Unmanaged": {Text: "Unmanaged", Color: "rgb(113, 113, 113)"},
				"Disabled":  {Text: "Disabled", Color: "rgb(100, 41, 0)"},
				"Testing":   {Text: "Testing", Color: "rgb(138, 0, 143)"},
			},
		},
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

	defer result.Body.Close()
	response.Frames = append(response.Frames, frame)
	return response
}

// Compare server version
func isVersionGreater(actualVersion, requireVersion string) bool {
	actualVersionParts := strings.Split(actualVersion, ".")
	requiredVersionParts := strings.Split(requireVersion, ".")
	maxLen := len(actualVersionParts)
	if len(requiredVersionParts) > maxLen {
		maxLen = len(requiredVersionParts)
	}
	for i := 0; i < maxLen; i++ {
		var actualVersionNum, requiredVersionNum int
		if i < len(actualVersionParts) {
			actualVersionNum, _ = strconv.Atoi(actualVersionParts[i])
		}
		if i < len(requiredVersionParts) {
			requiredVersionNum, _ = strconv.Atoi(requiredVersionParts[i])
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
func (d *NetXMSDatasource) CheckHealth(_ context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	res := &backend.CheckHealthResult{}
	config, err := models.LoadPluginSettings(*req.PluginContext.DataSourceInstanceSettings)

	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = "Unable to load settings"
		return res, nil
	}

	if config.Secrets.ApiKey == "" {
		res.Status = backend.HealthStatusError
		res.Message = "API key is missing"
		return res, nil
	}

	if config.ServerAddress == "" {
		res.Status = backend.HealthStatusError
		res.Message = "Server address is missing"
		return res, nil
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	statusURL := joinURL(config.ServerAddress, "v1/server-info")
	request, err := http.NewRequest("GET", statusURL, nil)
	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("Failed to create request: %v", err)
		return res, nil
	}

	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.Secrets.ApiKey))

	response, err := client.Do(request)
	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("Failed to connect to server: %v", err)
		return res, nil
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("failed read response: %d (%s)", response.StatusCode, response.Status)
		return res, nil
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("Server returned status code: %d (%s)", response.StatusCode, response.Status)
		return res, nil
	}

	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)
	actualVersion := data["version"].(string)
	requiredVersion := "5.2.4"
	if !isVersionGreater(actualVersion, requiredVersion) {
		fmt.Printf("Server version %s is NOT greater than %s\n", actualVersion, requiredVersion)
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("Server version (current: %s) should be greater than %s", actualVersion, requiredVersion)
		return res, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Data source is working",
	}, nil
}

func (ds *NetXMSDatasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	return ds.resourceHandler.CallResource(ctx, req, sender)
}

// This method handles all request to get lists of items in format name : id
func (ds *NetXMSDatasource) handleQuery(url string, method string, rw http.ResponseWriter, req *http.Request) {
	pCtx := backend.PluginConfigFromContext(req.Context())
	config, err := models.LoadPluginSettings(*pCtx.DataSourceInstanceSettings)
	if err != nil {
		http.Error(rw, "failed to load plugin settings", http.StatusInternalServerError)
		return
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	statusURL := joinURL(config.ServerAddress, url)
	request, err := http.NewRequest(method, statusURL, nil)
	if err != nil {
		http.Error(rw, "failed to create request", http.StatusInternalServerError)
		return
	}

	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.Secrets.ApiKey))

	result, err := client.Do(request)
	if err != nil {
		http.Error(rw, "failed to connect to server", http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(result.Body)
	if err != nil {
		http.Error(rw, "failed read response", http.StatusInternalServerError)
		return
	}
	defer result.Body.Close()

	// Parse JSON and sort by label
	var responseData map[string]interface{}
	if err := json.Unmarshal(body, &responseData); err != nil {
		// If parsing fails, return original response
		rw.Header().Add("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		rw.Write(body)
		return
	}

	// Check if "objects" field exists and is an array
	objects, ok := responseData["objects"].([]interface{})
	if !ok {
		// If no "objects" field, return original response
		rw.Header().Add("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		rw.Write(body)
		return
	}

	// Convert to slice of maps for sorting
	jsonData := make([]map[string]interface{}, len(objects))
	for i, obj := range objects {
		if objMap, ok := obj.(map[string]interface{}); ok {
			jsonData[i] = objMap
		} else {
			// If conversion fails, return original response
			rw.Header().Add("Content-Type", "application/json")
			rw.WriteHeader(http.StatusOK)
			rw.Write(body)
			return
		}
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
		http.Error(rw, "failed to marshal sorted response", http.StatusInternalServerError)
		return
	}

	rw.Header().Add("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)
	rw.Write(sortedBody)
}

func (ds *NetXMSDatasource) handleAlarmObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/object-list?filter=alarm", "GET", rw, req)
}

func (ds *NetXMSDatasource) handleDciObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/object-list?filter=dci", "GET", rw, req)
}

func (ds *NetXMSDatasource) handleSummaryTables(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/summary-table-list", "GET", rw, req)
}

func (ds *NetXMSDatasource) handleSummaryTableObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/object-list?filter=summary", "GET", rw, req)
}

func (ds *NetXMSDatasource) handleObjectQueries(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/query-list", "GET", rw, req)
}

func (ds *NetXMSDatasource) handleObjectQueryObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/grafana/object-list?filter=query", "GET", rw, req)
}

func (ds *NetXMSDatasource) handleDciList(rw http.ResponseWriter, req *http.Request) {
	objectID := req.URL.Query().Get("objectId")
	if objectID == "" {
		http.Error(rw, "missing objectId parameter", http.StatusBadRequest)
		return
	}
	path := fmt.Sprintf("/v1/grafana/objects/%s/dci-list", objectID)
	ds.handleQuery(path, "GET", rw, req)
}

func (ds *NetXMSDatasource) handleDciValues(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		var qm queryModel
		if err := json.Unmarshal(q.JSON, &qm); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err.Error()))
			continue
		}

		config, err := models.LoadPluginSettings(*req.PluginContext.DataSourceInstanceSettings)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to load plugin settings: %v", err))
			continue
		}

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		timeFrom := q.TimeRange.From.Format(time.UnixDate)
		timeTo := q.TimeRange.To.Format(time.UnixDate)

		url := joinURL(config.ServerAddress, fmt.Sprintf("v1/objects/%s/data-collection/%s/history?timeFrom=%s&timeTo=%s",
			qm.SourceObjectId, qm.DciId, timeFrom, timeTo))

		request, err := http.NewRequest("GET", url, nil)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to create request: %v", err))
			continue
		}

		request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.Secrets.ApiKey))

		result, err := client.Do(request)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to connect to server: %v", err))
			continue
		}
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to read response: %v", err))
			continue
		}

		var dciData dciValueResponse
		if err := json.Unmarshal(body, &dciData); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err))
			continue
		}

		frame := data.NewFrame(dciData.Description)

		times := make([]time.Time, len(dciData.Values))
		values := make([]float64, len(dciData.Values))

		for i, v := range dciData.Values {
			t, err := time.Parse(time.RFC3339, v.Timestamp)
			if err != nil {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse timestamp: %v", err))
				continue
			}
			times[i] = t

			val, err := strconv.ParseFloat(v.Value, 64)
			if err != nil {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse value: %v", err))
				continue
			}
			values[i] = val
		}

		frame.Fields = append(frame.Fields,
			data.NewField("time", nil, times),
			data.NewField("value", map[string]string{"unit": dciData.UnitName}, values),
		)

		response.Responses[q.RefID] = backend.DataResponse{
			Frames: data.Frames{frame},
		}
	}
	return response, nil
}

type orderedMap struct {
	keys   []string
	values map[string]interface{}
}

func decodeOrderedJSON(data []byte) (*orderedMap, error) {
	om := &orderedMap{
		keys:   make([]string, 0),
		values: make(map[string]interface{}),
	}

	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected object, got %v", token)
	}

	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		keyStr, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("expected string key, got %v", key)
		}

		var value interface{}
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}

		om.keys = append(om.keys, keyStr)
		om.values[keyStr] = value
	}

	if _, err := dec.Token(); err != nil {
		return nil, err
	}

	return om, nil
}

func (d *NetXMSDatasource) handleTableQuery(_ context.Context, req *backend.QueryDataRequest, queryConfig tableQueryConfig) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()

	for _, q := range req.Queries {
		var qm map[string]interface{}
		if err := json.Unmarshal(q.JSON, &qm); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err.Error()))
			continue
		}

		for field, message := range queryConfig.required {
			if value, ok := qm[field].(string); !ok || value == "" {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, message)
				continue
			}
		}

		pluginConfig, err := models.LoadPluginSettings(*req.PluginContext.DataSourceInstanceSettings)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to load plugin settings: %v", err))
			continue
		}

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		url := joinURL(pluginConfig.ServerAddress, queryConfig.url)

		reqBody := queryConfig.formatBody(qm)

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to marshal request body: %v", err))
			continue
		}

		request, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to create request: %v", err))
			continue
		}

		request.Header.Set("Content-Type", "application/json")
		request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", pluginConfig.Secrets.ApiKey))

		result, err := client.Do(request)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to connect to server: %v", err))
			continue
		}
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to read response: %v", err))
			continue
		}

		if result.StatusCode == http.StatusUnauthorized {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusUnauthorized, "Unauthorized: Invalid API key")
			continue
		}

		if result.StatusCode != http.StatusOK {
			var reasonResp map[string]string
			if err := json.Unmarshal(body, &reasonResp); err == nil {
				response.Responses[q.RefID] = backend.ErrDataResponse(httpStatusToBackendStatus(result.StatusCode), fmt.Sprintf("Request error: %s", reasonResp["reason"]))
				continue
			}
			response.Responses[q.RefID] = backend.ErrDataResponse(httpStatusToBackendStatus(result.StatusCode), "Request error")
			continue
		}

		var tableResponse []map[string]interface{}
		if err := json.Unmarshal(body, &tableResponse); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err))
			continue
		}

		frame := data.NewFrame(queryConfig.frameName)

		if len(tableResponse) > 0 {
			dec := json.NewDecoder(bytes.NewReader(body))
			if token, err := dec.Token(); err != nil || token != json.Delim('[') {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("expected array, got %v", token))
				continue
			}

			if !dec.More() {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, "empty array")
				continue
			}

			var firstObject json.RawMessage
			if err := dec.Decode(&firstObject); err != nil {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to decode first object: %v", err))
				continue
			}

			orderedData, err := decodeOrderedJSON(firstObject)
			if err != nil {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse first row: %v", err))
				continue
			}

			columnValues := make(map[string][]interface{})
			for _, columnName := range orderedData.keys {
				columnValues[columnName] = make([]interface{}, len(tableResponse))
			}

			for i, row := range tableResponse {
				for _, columnName := range orderedData.keys {
					val := row[columnName]
					if val == nil {
						columnValues[columnName][i] = nil
						continue
					}

					switch v := val.(type) {
					case string:
						columnValues[columnName][i] = v
					case float64:
						columnValues[columnName][i] = v
					case int:
						columnValues[columnName][i] = float64(v)
					case int64:
						columnValues[columnName][i] = float64(v)
					case bool:
						columnValues[columnName][i] = v
					case []interface{}:
						columnValues[columnName][i] = fmt.Sprintf("%v", v)
					default:
						columnValues[columnName][i] = fmt.Sprintf("%v", v)
					}
				}
			}

			for _, columnName := range orderedData.keys {
				values := columnValues[columnName]
				var field *data.Field
				if len(values) > 0 && values[0] != nil {
					switch values[0].(type) {
					case float64:
						field = data.NewField(columnName, nil, values)
					case bool:
						field = data.NewField(columnName, nil, values)
					default:
						strValues := make([]string, len(values))
						for i, v := range values {
							if v == nil {
								strValues[i] = ""
							} else {
								strValues[i] = fmt.Sprintf("%v", v)
							}
						}
						field = data.NewField(columnName, nil, strValues)
					}
				} else {
					field = data.NewField(columnName, nil, make([]string, len(values)))
				}
				frame.Fields = append(frame.Fields, field)
			}
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
		required: map[string]string{
			"summaryTableId": "tableId is required",
		},
		formatBody: func(qm map[string]interface{}) map[string]interface{} {
			reqBody := make(map[string]interface{})

			if rootObjectId, ok := qm["sourceObjectId"].(string); ok && rootObjectId != "" {
				rootObjectIdNum, err := strconv.ParseInt(rootObjectId, 10, 64)
				if err == nil {
					reqBody["rootObjectId"] = rootObjectIdNum
				}
			}

			if tableId, ok := qm["summaryTableId"].(string); ok && tableId != "" {
				tableIdNum, err := strconv.ParseInt(tableId, 10, 64)
				if err == nil {
					reqBody["tableId"] = tableIdNum
				}
			}

			return reqBody
		},
	})
}

func (d *NetXMSDatasource) handleObjectQueryQuery(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	return d.handleTableQuery(ctx, req, tableQueryConfig{
		url:       "/v1/grafana/infinity/object-query",
		frameName: "object-query",
		required: map[string]string{
			"objectQueryId":  "queryId is required",
			"sourceObjectId": "rootObjectId is required",
		},
		formatBody: func(qm map[string]interface{}) map[string]interface{} {
			reqBody := make(map[string]interface{})

			if rootObjectId, ok := qm["sourceObjectId"].(string); ok && rootObjectId != "" {
				rootObjectIdNum, err := strconv.ParseInt(rootObjectId, 10, 64)
				if err == nil {
					reqBody["rootObjectId"] = rootObjectIdNum
				}
			}

			if queryId, ok := qm["objectQueryId"].(string); ok && queryId != "" {
				queryIdNum, err := strconv.ParseInt(queryId, 10, 64)
				if err == nil {
					reqBody["queryId"] = queryIdNum
				}
			}

			if values, ok := qm["queryParameters"].(string); ok && values != "" {
				var parsedValues []map[string]interface{}
				if err := json.Unmarshal([]byte(values), &parsedValues); err == nil {
					reqBody["values"] = parsedValues
				}
			}

			return reqBody
		},
	})
}

type objectStatusResponse struct {
	Name   string `json:"Name"`
	Status int32  `json:"Status"`
}

func (d *NetXMSDatasource) handleObjectStatusQuery(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()

	for _, q := range req.Queries {
		var qm queryModel
		if err := json.Unmarshal(q.JSON, &qm); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err.Error()))
			continue
		}

		pluginConfig, err := models.LoadPluginSettings(*req.PluginContext.DataSourceInstanceSettings)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to load plugin settings: %v", err))
			continue
		}

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		url := joinURL(pluginConfig.ServerAddress, "/v1/grafana/objects-status")

		reqBody := map[string]interface{}{}
		if qm.SourceObjectId != "" {
			rootObjectIdNum, err := strconv.ParseInt(qm.SourceObjectId, 10, 64)
			if err == nil {
				reqBody["rootObjectId"] = rootObjectIdNum
			}
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to marshal request body: %v", err))
			continue
		}

		request, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to create request: %v", err))
			continue
		}

		request.Header.Set("Content-Type", "application/json")
		request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", pluginConfig.Secrets.ApiKey))

		result, err := client.Do(request)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to connect to server: %v", err))
			continue
		}
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to read response: %v", err))
			continue
		}

		if result.StatusCode == http.StatusUnauthorized {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusUnauthorized, "Unauthorized: Invalid API key")
			continue
		}

		if result.StatusCode != http.StatusOK {
			var reasonResp map[string]string
			if err := json.Unmarshal(body, &reasonResp); err == nil {
				response.Responses[q.RefID] = backend.ErrDataResponse(httpStatusToBackendStatus(result.StatusCode), fmt.Sprintf("Request error: %s", reasonResp["reason"]))
				continue
			}
			response.Responses[q.RefID] = backend.ErrDataResponse(httpStatusToBackendStatus(result.StatusCode), "Request error")
			continue
		}

		var statusData []objectStatusResponse
		if err := json.Unmarshal([]byte(body), &statusData); err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err))
			continue
		}

		color := []string{
			"rgb(0, 137, 0)",     // Normal
			"rgb(0, 142, 145)",   // Warning
			"rgb(201, 198, 0)",   // Minor
			"rgb(223, 102, 0)",   // Major
			"rgb(160, 0, 0)",     // Critical
			"rgb(33, 33, 248)",   // Unknown
			"rgb(113, 113, 113)", // Unmanaged
			"rgb(100, 41, 0)",    // Disabled
			"rgb(138, 0, 143)",   // Testing
		}

		var frames data.Frames
		for _, obj := range statusData {
			frame := data.NewFrame(obj.Name)

			// Use DisplayName to show object name in stat panel
			nameField := data.NewField("Name", nil, []string{obj.Name})
			nameField.Config = &data.FieldConfig{
				Mappings: data.ValueMappings{
					data.ValueMapper{
						obj.Name: {Text: obj.Name, Color: color[obj.Status]},
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

func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	path = strings.TrimLeft(path, "/")
	return base + "/" + path
}
