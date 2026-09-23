package plugin

import (
	"context"
	"errors"
	"net/http"
	"strings"
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
	config       providerConfig
	provider     LLMProvider
	client       *http.Client
	resourceGate *providerResourceGate
}

func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	config, err := parseProviderConfig(settings)
	if err != nil {
		return nil, err
	}
	client := newProviderHTTPClient()
	provider, err := newLLMProvider(config, client)
	if err != nil {
		return nil, err
	}
	return newDatasourceWithProviderAndClient(config, provider, client), nil
}

func newDatasource(config providerConfig, client *http.Client) *Datasource {
	provider, err := newLLMProvider(config, client)
	if err != nil {
		provider = unavailableLLMProvider{err: err}
	}
	return newDatasourceWithProviderAndClient(config, provider, client)
}

func newDatasourceWithProvider(config providerConfig, provider LLMProvider) *Datasource {
	return newDatasourceWithProviderAndClient(config, provider, nil)
}

func newDatasourceWithProviderAndClient(config providerConfig, provider LLMProvider, client *http.Client) *Datasource {
	ds := &Datasource{
		config:       config,
		provider:     provider,
		client:       client,
		resourceGate: newProviderResourceGate(maxConcurrentProviderRequests, maxProviderRequestsPerWindow, providerRequestWindow),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", ds.handleHealth)
	mux.Handle("/models", ds.resourceGate.wrap(http.HandlerFunc(ds.handleModels)))
	mux.Handle("/models/preview", requireGrafanaAdmin(ds.resourceGate.wrap(http.HandlerFunc(ds.handleModelsPreview))))
	mux.Handle("/test", requireGrafanaAdmin(ds.resourceGate.wrap(http.HandlerFunc(ds.handleTestConnection))))
	mux.Handle("/chat", ds.resourceGate.wrap(http.HandlerFunc(ds.handleChat)))
	mux.Handle("/generate", ds.resourceGate.wrap(http.HandlerFunc(ds.handleGenerate)))
	mux.Handle("/repair", ds.resourceGate.wrap(http.HandlerFunc(ds.handleRepair)))
	ds.CallResourceHandler = httpadapter.New(mux)
	return ds
}

func newProviderHTTPClient() *http.Client {
	return &http.Client{
		Transport:     newProviderTransport(),
		Timeout:       upstreamTimeout,
		CheckRedirect: rejectCrossHostRedirect,
	}
}

func rejectCrossHostRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) == 0 {
		return nil
	}
	origin := via[0].URL
	if !strings.EqualFold(request.URL.Scheme, origin.Scheme) || !strings.EqualFold(request.URL.Host, origin.Host) {
		return http.ErrUseLastResponse
	}
	return nil
}

type unavailableLLMProvider struct{ err error }

func (provider unavailableLLMProvider) Chat(context.Context, chatRequest) (string, error) {
	return "", provider.err
}

func (provider unavailableLLMProvider) Generate(context.Context, generateChartRequest) (generateChartResponse, error) {
	return generateChartResponse{}, provider.err
}

func (provider unavailableLLMProvider) Repair(context.Context, repairChartRequest) (generateChartResponse, error) {
	return generateChartResponse{}, provider.err
}

func (provider unavailableLLMProvider) ListModels(context.Context) ([]string, error) {
	return nil, provider.err
}

func (provider unavailableLLMProvider) TestConnection(context.Context) error {
	return provider.err
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
			Error: errors.New("this Flint AI connection is for Panel AI Assist, not a business query data source"),
		}
	}
	return response, nil
}
