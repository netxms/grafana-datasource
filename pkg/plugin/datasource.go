package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
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
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// Datasource is an example datasource which can respond to data queries, reports
// its health and has streaming skills.
type Datasource struct {
	queryHandler    backend.QueryDataHandler
	resourceHandler backend.CallResourceHandler
}

// NewDatasource creates a new datasource instance.
func NewDatasource(_ context.Context, _ backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	ds := &Datasource{}
	mux := http.NewServeMux()
	mux.HandleFunc("/alarmObjects", ds.handleAlarmObjects)
	mux.HandleFunc("/dciObjects", ds.handleDciObjects)
	mux.HandleFunc("/objectQueryObjects", ds.handleObjectQueryObjects)
	mux.HandleFunc("/summaryTableObjects", ds.handleSummaryTableObjects)
	mux.HandleFunc("/dcis", ds.handleDciList)
	ds.resourceHandler = httpadapter.New(mux)
	queryTypeMux := datasource.NewQueryTypeMux()
	queryTypeMux.HandleFunc("alarms", ds.handleAlarmQuery)
	queryTypeMux.HandleFunc("dciValues", ds.handleDciValues)
	//queryTypeMux.HandleFunc("summaryTables", ds.handleSummaryTableQuery)
	//queryTypeMux.HandleFunc("objectQueries", ds.handleObjectQueryQuery)
	//TODO: should I do fallback? queryTypeMux.HandleFunc("", ds.handleQueryFallback)
	ds.queryHandler = queryTypeMux
	return ds, nil
}

// Dispose here tells plugin SDK that plugin wants to clean up resources when a new instance
// created. As soon as datasource settings change detected by SDK old datasource instance will
// be disposed and a new one will be created using NewSampleDatasource factory function.
func (d *Datasource) Dispose() {
	// Clean up datasource instance resources.
}

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

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	return d.queryHandler.QueryData(ctx, req)
}

// QueryData handles multiple queries and returns multiple responses.
// req contains the queries []DataQuery (where each query contains RefID as a unique identifier).
// The QueryDataResponse contains a map of RefID to the response for each query, and each response
// contains Frames ([]*Frame).
func (d *Datasource) handleAlarmQuery(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	// create response struct
	response := backend.NewQueryDataResponse()

	// loop over queries and execute them individually.
	for _, q := range req.Queries {
		// Unmarshal the JSON into our queryModel.
		var qm queryModel

		err := json.Unmarshal(q.JSON, &qm)

		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err.Error()))
			continue
		}

		res := d.query(ctx, req.PluginContext, q, qm.SourceObjectId)

		// save the response in a hashmap
		// based on with RefID as identifier
		response.Responses[q.RefID] = res
	}

	return response, nil
}

func (d *Datasource) query(_ context.Context, pCtx backend.PluginContext, query backend.DataQuery, rootObjectId string) backend.DataResponse {
	var response backend.DataResponse
	config, err := models.LoadPluginSettings(*pCtx.DataSourceInstanceSettings)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to load plugin settings: %v", err))
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Create request to status endpoint
	statusURL := fmt.Sprintf("%s%s", config.ServerAddress, "/v1/grafana/infinity/alarms")

	// Prepare JSON body with rootObjectId if not empty
	var bodyBytes []byte
	if rootObjectId != "" {
		// Convert rootObjectId to number
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

	// Add API key to headers
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.Secrets.ApiKey))

	// Make the request
	result, err := client.Do(request)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to connect to server: %v", err.Error()))
	}

	body, err := io.ReadAll(result.Body)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed read response: %v", err.Error()))
	}

	// Check for error status codes
	if result.StatusCode == http.StatusUnauthorized {
		return backend.ErrDataResponse(backend.StatusUnauthorized, "Unauthorized: Invalid API key")
	}

	var alarms []alarmResponse
	if err := json.Unmarshal(body, &alarms); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to parse response: %v", err.Error()))
	}

	// Create data frame
	frame := data.NewFrame("alarms")

	// Prepare arrays for each field
	ids := make([]int32, len(alarms))
	severities := make([]string, len(alarms))
	states := make([]string, len(alarms))
	sources := make([]string, len(alarms))
	messages := make([]string, len(alarms))
	counts := make([]int32, len(alarms))
	ackBy := make([]string, len(alarms))
	created := make([]time.Time, len(alarms))
	lastChange := make([]time.Time, len(alarms))

	// Fill arrays with data
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

	// Add fields to the frame
	frame.Fields = append(frame.Fields,
		data.NewField("Id", nil, ids),
		data.NewField("Severity", nil, severities),
		data.NewField("State", nil, states),
		data.NewField("Source", nil, sources),
		data.NewField("Message", nil, messages),
		data.NewField("Count", nil, counts),
		data.NewField("Ack/Resolve by", nil, ackBy),
		data.NewField("Created", nil, created),
		data.NewField("Last Change", nil, lastChange),
	)

	defer result.Body.Close()

	// add the frames to the response.
	response.Frames = append(response.Frames, frame)

	return response
}

// CheckHealth handles health checks sent from Grafana to the plugin.
// The main use case for these health checks is the test button on the
// datasource configuration page which allows users to verify that
// a datasource is working as expected.
func (d *Datasource) CheckHealth(_ context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
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

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Create request to status endpoint
	statusURL := fmt.Sprintf("%s/v1/status", config.ServerAddress)
	request, err := http.NewRequest("GET", statusURL, nil)
	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("Failed to create request: %v", err)
		return res, nil
	}

	// Add API key to headers
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.Secrets.ApiKey))

	// Make the request
	response, err := client.Do(request)
	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("Failed to connect to server: %v", err)
		return res, nil
	}
	defer response.Body.Close()

	// Check response status
	if response.StatusCode != http.StatusOK {
		res.Status = backend.HealthStatusError
		res.Message = fmt.Sprintf("Server returned status code: %d (%s)", response.StatusCode, response.Status)
		return res, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Data source is working",
	}, nil
}

func (ds *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	return ds.resourceHandler.CallResource(ctx, req, sender)
}

func (ds *Datasource) handleQuery(url string, method string, rw http.ResponseWriter, req *http.Request) {
	log.Printf("handleQuery called: url=%s, method=%s", url, method)
	pCtx := backend.PluginConfigFromContext(req.Context())
	config, err := models.LoadPluginSettings(*pCtx.DataSourceInstanceSettings)
	if err != nil {
		log.Printf("failed to load plugin settings: %v", err)
		http.Error(rw, "failed to load plugin settings", http.StatusInternalServerError)
		return
	}
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Create request to status endpoint
	statusURL := fmt.Sprintf("%s%s", config.ServerAddress, url)
	log.Printf("Loaded config: ServerAddress=%s", statusURL)
	request, err := http.NewRequest(method, statusURL, nil)
	if err != nil {
		log.Printf("failed to create request=%s", statusURL)
		http.Error(rw, "failed to create request", http.StatusInternalServerError)
		return
	}

	// Add API key to headers
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.Secrets.ApiKey))

	// Make the request
	result, err := client.Do(request)
	if err != nil {
		log.Printf("Failed to connect to server: %v", err)
		http.Error(rw, "failed to connect to server", http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(result.Body)
	if err != nil {
		log.Printf("failed read response")
		http.Error(rw, "failed read response", http.StatusInternalServerError)
		return
	}
	rw.Header().Add("Content-Type", "application/json")
	log.Printf("bodey %s", string(body))
	_, err = rw.Write(body)
	if err != nil {
		log.Printf("failed write response")
		return
	}
	defer result.Body.Close()
	rw.WriteHeader(http.StatusOK)
}

func (ds *Datasource) handleAlarmObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/object-list?filter=alarm", "GET", rw, req)
}

func (ds *Datasource) handleDciObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/object-list?filter=dci", "GET", rw, req)
}

func (ds *Datasource) handleSummaryTableObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/object-list?filter=summary", "GET", rw, req)
}

func (ds *Datasource) handleObjectQueryObjects(rw http.ResponseWriter, req *http.Request) {
	ds.handleQuery("/v1/object-list?filter=query", "GET", rw, req)
}

func (ds *Datasource) handleDciList(rw http.ResponseWriter, req *http.Request) {
	log.Printf("#### Came to handleDciList")
	objectID := req.URL.Query().Get("objectId")
	if objectID == "" {
		http.Error(rw, "missing objectId parameter", http.StatusBadRequest)
		return
	}
	path := fmt.Sprintf("/v1/objects/%s/dci-list", objectID)
	ds.handleQuery(path, "GET", rw, req)
}

func (ds *Datasource) handleDciValues(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
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

		// Create HTTP client with timeout
		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		// Format time range
		timeFrom := q.TimeRange.From.Format(time.UnixDate)
		timeTo := q.TimeRange.To.Format(time.UnixDate)

		// Create request URL
		url := fmt.Sprintf("%s/v1/objects/%s/data-collection/%s/history?timeFrom=%s&timeTo=%s",
			config.ServerAddress, qm.SourceObjectId, qm.DciId, timeFrom, timeTo)

		request, err := http.NewRequest("GET", url, nil)
		if err != nil {
			response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("failed to create request: %v", err))
			continue
		}

		// Add API key to headers
		request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.Secrets.ApiKey))

		// Make the request
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

		// Create data frame
		frame := data.NewFrame(dciData.Description)

		// Add time field
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
