package plugin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"
)

const upstreamTimeout = 30 * time.Second

var (
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

type Datasource struct {
	backend.CallResourceHandler
	config providerConfig
	client *http.Client
}

func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	config, err := parseProviderConfig(settings)
	if err != nil {
		return nil, err
	}
	return newDatasource(config, &http.Client{Timeout: upstreamTimeout}), nil
}

func newDatasource(config providerConfig, client *http.Client) *Datasource {
	ds := &Datasource{config: config, client: client}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", ds.handleHealth)
	mux.HandleFunc("/chat", ds.handleChat)
	mux.HandleFunc("/generate", ds.handleGenerate)
	ds.CallResourceHandler = httpadapter.New(mux)
	return ds
}

func (ds *Datasource) Dispose() {}

func (ds *Datasource) CheckHealth(_ context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if err := ds.config.validate(); err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: err.Error()}, nil
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Flint AI provider configuration is ready; no billable remote request was made",
	}, nil
}

func (ds *Datasource) QueryData(_ context.Context, request *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, query := range request.Queries {
		response.Responses[query.RefID] = backend.DataResponse{
			Error: errors.New("Flint AI is a provider connection for Panel AI Assist, not a business query data source"),
		}
	}
	return response, nil
}
