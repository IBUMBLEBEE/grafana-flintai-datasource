package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type fakeLLMProvider struct {
	models     []string
	modelsErr  error
	modelsHits int
	testErr    error
	testHits   int
}

func (provider *fakeLLMProvider) Chat(context.Context, chatRequest) (string, error) {
	return "", errors.New("unexpected Chat call")
}

func (provider *fakeLLMProvider) Generate(context.Context, generateChartRequest) (generateChartResponse, error) {
	return generateChartResponse{}, errors.New("unexpected Generate call")
}

func (provider *fakeLLMProvider) Repair(context.Context, repairChartRequest) (generateChartResponse, error) {
	return generateChartResponse{}, errors.New("unexpected Repair call")
}

func (provider *fakeLLMProvider) ListModels(context.Context) ([]string, error) {
	provider.modelsHits++
	return provider.models, provider.modelsErr
}

func (provider *fakeLLMProvider) TestConnection(context.Context) error {
	provider.testHits++
	return provider.testErr
}

func jsonHTTPClient(status int, body string, inspect func(*http.Request)) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if inspect != nil {
			inspect(request)
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
}

func validConfig(baseURL string) providerConfig {
	return providerConfig{
		baseURL:      baseURL,
		model:        "test-model",
		providerKind: providerKindOpenAI,
		apiKey:       "test-secret",
	}
}

func validGenerateRequest() generateChartRequest {
	return generateChartRequest{
		Prompt: "make a chart",
		Fields: []generateField{
			{Name: "region", Type: "string"},
			{Name: "revenue", Type: "number"},
		},
		SuggestedChartType: "Bar Chart",
		RenderBackend:      "echarts",
		ChartCatalog: []chartCatalogEntry{
			{ChartType: "Bar Chart", Channels: []string{"x", "y", "color"}, RequiredChannels: []string{"x", "y"}, Properties: []chartPropertyCatalog{}},
			{ChartType: "Pie Chart", Channels: []string{"size", "color"}, RequiredChannels: []string{"size", "color"}, Properties: []chartPropertyCatalog{}},
		},
		SemanticTypes: []string{"Category", "Amount", "Quantity"},
		SampleRows:    []map[string]any{{"region": "North", "revenue": float64(120)}},
		FrameSummary:  []frameSummary{{FrameIndex: 0, RefID: "A", Fields: []string{"region", "revenue"}}},
	}
}

func requestBody(t *testing.T, input generateChartRequest) string {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return string(body)
}

func TestParseProviderConfigReadsOnlyDatasourceSettings(t *testing.T) {
	config, err := parseProviderConfig(backend.DataSourceInstanceSettings{
		JSONData:                []byte(`{"baseUrl":"https://api.example.test/v1/","model":" test-model ","providerKind":"openai"}`),
		DecryptedSecureJSONData: map[string]string{"openaiApiKey": " test-secret "},
	})
	if err != nil {
		t.Fatalf("parse provider config: %v", err)
	}
	if config.baseURL != "https://api.example.test/v1" || config.model != "test-model" || config.apiKey != "test-secret" || config.providerKind != providerKindOpenAI {
		t.Fatalf("unexpected provider config: %#v", config)
	}
	if err := config.validate(); err != nil {
		t.Fatalf("validate provider config: %v", err)
	}
}

func TestProviderConfigRejectsHTTPWithoutExplicitLocalOptIn(t *testing.T) {
	config := validConfig("http://localhost:8080/v1")
	if err := config.validate(); err == nil {
		t.Fatal("expected HTTP provider URL to be rejected")
	}
	t.Setenv(allowHTTPEnv, "true")
	if err := config.validate(); err != nil {
		t.Fatalf("expected explicit local HTTP opt-in to work: %v", err)
	}
	config.baseURL = "http://provider.example.test/v1"
	if err := config.validate(); err == nil {
		t.Fatal("expected non-local HTTP provider URL to remain rejected")
	}
}

func TestHealthReportsMisconfiguredInstanceWithoutSecrets(t *testing.T) {
	ds := newDatasource(providerConfig{}, http.DefaultClient)
	recorder := httptest.NewRecorder()
	ds.handleHealth(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"configured\":false}\n" {
		t.Fatalf("unexpected health response: %d %q", recorder.Code, recorder.Body.String())
	}

	result, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil || result.Status != backend.HealthStatusError || strings.Contains(result.Message, "test-secret") {
		t.Fatalf("unexpected CheckHealth result: %#v, %v", result, err)
	}
}

func TestModelsResourceDelegatesThroughLLMProviderInterface(t *testing.T) {
	config := validConfig("https://api.example.test/v1")
	config.model = ""
	provider := &fakeLLMProvider{models: []string{"model-a", "model-b"}}
	ds := newDatasourceWithProvider(config, provider)
	recorder := httptest.NewRecorder()

	ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))

	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"models\":[\"model-a\",\"model-b\"]}\n" {
		t.Fatalf("unexpected models response: %d %s", recorder.Code, recorder.Body.String())
	}
	if provider.modelsHits != 1 {
		t.Fatalf("expected one provider call, got %d", provider.modelsHits)
	}
}

func TestModelsPreviewUsesTransientConnectionWithoutPersistingIt(t *testing.T) {
	var gotURL string
	var gotAuth string
	ds := newDatasource(validConfig("https://saved.example.test/v1"), jsonHTTPClient(http.StatusOK, `{"data":[{"id":"deepseek-chat"}]}`, func(request *http.Request) {
		gotURL = request.URL.String()
		gotAuth = request.Header.Get("Authorization")
	}))
	recorder := httptest.NewRecorder()
	body := `{"providerKind":"deepseek","baseUrl":"https://preview.example.test/v1","apiKey":"preview-secret"}`

	ds.handleModelsPreview(recorder, httptest.NewRequest(http.MethodPost, "/models/preview", strings.NewReader(body)))

	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"models\":[\"deepseek-chat\"]}\n" {
		t.Fatalf("unexpected preview response: %d %s", recorder.Code, recorder.Body.String())
	}
	if gotURL != "https://preview.example.test/models" || gotAuth != "Bearer preview-secret" {
		t.Fatalf("preview did not use transient connection: url=%q auth=%q", gotURL, gotAuth)
	}
	if ds.config.baseURL != "https://saved.example.test/v1" || ds.config.apiKey != "test-secret" {
		t.Fatalf("preview mutated saved config: %#v", ds.config)
	}
}

func TestModelsPreviewRejectsInvalidInputAndRedactsTransientSecret(t *testing.T) {
	t.Run("method not allowed", func(t *testing.T) {
		ds := newDatasource(validConfig("https://saved.example.test/v1"), http.DefaultClient)
		recorder := httptest.NewRecorder()
		ds.handleModelsPreview(recorder, httptest.NewRequest(http.MethodGet, "/models/preview", nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("unexpected status: %d", recorder.Code)
		}
	})

	for _, body := range []string{
		`{"providerKind":"openai","baseUrl":"https://api.example.test/v1","apiKey":""}`,
		`{"providerKind":"gemini","baseUrl":"https://api.example.test/v1","apiKey":"secret"}`,
		`{"providerKind":"openai","baseUrl":"https://api.example.test/v1","apiKey":"secret","unknown":true}`,
	} {
		ds := newDatasource(validConfig("https://saved.example.test/v1"), http.DefaultClient)
		recorder := httptest.NewRecorder()
		ds.handleModelsPreview(recorder, httptest.NewRequest(http.MethodPost, "/models/preview", strings.NewReader(body)))
		if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Body.String(), "secret") {
			t.Fatalf("invalid preview was not rejected safely: %d %s", recorder.Code, recorder.Body.String())
		}
	}

	t.Run("provider error redacts transient key", func(t *testing.T) {
		ds := newDatasource(validConfig("https://saved.example.test/v1"), jsonHTTPClient(http.StatusUnauthorized, `{"error":{"message":"bad key preview-secret"}}`, nil))
		recorder := httptest.NewRecorder()
		body := `{"providerKind":"openai","baseUrl":"https://preview.example.test/v1","apiKey":"preview-secret"}`
		ds.handleModelsPreview(recorder, httptest.NewRequest(http.MethodPost, "/models/preview", strings.NewReader(body)))
		if recorder.Code != http.StatusUnauthorized || strings.Contains(recorder.Body.String(), "preview-secret") || !strings.Contains(recorder.Body.String(), "<redacted>") {
			t.Fatalf("unexpected preview auth error: %d %s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestConnectionUsesCurrentSettingsWithoutPersistingThem(t *testing.T) {
	t.Run("saved key with edited model", func(t *testing.T) {
		var received upstreamRequest
		var gotAuth string
		var gotURL string
		client := jsonHTTPClient(http.StatusOK, `{"choices":[{"message":{"content":"OK"}}]}`, func(request *http.Request) {
			gotURL = request.URL.String()
			gotAuth = request.Header.Get("Authorization")
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Errorf("decode provider request: %v", err)
			}
		})
		ds := newDatasource(validConfig("https://api.example.test/v1"), client)
		recorder := httptest.NewRecorder()
		body := `{"providerKind":"openai","baseUrl":"https://api.example.test/v1","model":"custom-model"}`

		ds.handleTestConnection(recorder, httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body)))

		if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"ok\":true,\"message\":\"AI connection succeeded\"}\n" {
			t.Fatalf("unexpected test response: %d %s", recorder.Code, recorder.Body.String())
		}
		if gotURL != "https://api.example.test/v1/chat/completions" || gotAuth != "Bearer test-secret" {
			t.Fatalf("test did not use saved connection safely: url=%q auth=%q", gotURL, gotAuth)
		}
		if received.Model != "custom-model" || received.MaxCompletionTokens != maxProviderTestTokens || received.MaxTokens != 0 {
			t.Fatalf("unexpected OpenAI test request: %#v", received)
		}
		if ds.config.model != "test-model" {
			t.Fatalf("test mutated saved config: %#v", ds.config)
		}
	})

	t.Run("transient DeepSeek connection", func(t *testing.T) {
		var received upstreamRequest
		var gotAuth string
		client := jsonHTTPClient(http.StatusOK, `{"choices":[{"message":{"content":"OK"}}]}`, func(request *http.Request) {
			gotAuth = request.Header.Get("Authorization")
			if request.URL.String() != "https://preview.example.test/v1/chat/completions" {
				t.Errorf("unexpected provider URL: %s", request.URL)
			}
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Errorf("decode provider request: %v", err)
			}
		})
		ds := newDatasource(validConfig("https://saved.example.test/v1"), client)
		recorder := httptest.NewRecorder()
		body := `{"providerKind":"deepseek","baseUrl":"https://preview.example.test/v1","model":"deepseek-chat","apiKey":"preview-secret"}`

		ds.handleTestConnection(recorder, httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body)))

		if recorder.Code != http.StatusOK || gotAuth != "Bearer preview-secret" {
			t.Fatalf("unexpected transient test response: %d %s auth=%q", recorder.Code, recorder.Body.String(), gotAuth)
		}
		if received.Model != "deepseek-chat" || received.MaxTokens != maxProviderTestTokens || received.MaxCompletionTokens != 0 {
			t.Fatalf("unexpected DeepSeek test request: %#v", received)
		}
	})
}

func TestConnectionRejectsUnsafeOrInvalidRequests(t *testing.T) {
	t.Run("does not reuse a saved key for an edited Base URL", func(t *testing.T) {
		called := false
		ds := newDatasource(validConfig("https://saved.example.test/v1"), jsonHTTPClient(http.StatusOK, `{}`, func(*http.Request) {
			called = true
		}))
		recorder := httptest.NewRecorder()
		body := `{"providerKind":"openai","baseUrl":"https://other.example.test/v1","model":"test-model"}`

		ds.handleTestConnection(recorder, httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body)))

		if recorder.Code != http.StatusBadRequest || called || !strings.Contains(recorder.Body.String(), "API key is not configured") {
			t.Fatalf("unsafe saved-key reuse was not rejected: %d %s called=%v", recorder.Code, recorder.Body.String(), called)
		}
	})

	t.Run("redacts a transient key from provider errors", func(t *testing.T) {
		ds := newDatasource(validConfig("https://saved.example.test/v1"), jsonHTTPClient(http.StatusUnauthorized, `{"error":{"message":"bad key preview-secret"}}`, nil))
		recorder := httptest.NewRecorder()
		body := `{"providerKind":"openai","baseUrl":"https://preview.example.test/v1","model":"test-model","apiKey":"preview-secret"}`

		ds.handleTestConnection(recorder, httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body)))

		if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), "preview-secret") || !strings.Contains(recorder.Body.String(), "<redacted>") {
			t.Fatalf("unexpected connection test error: %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("rejects invalid method and unknown fields", func(t *testing.T) {
		ds := newDatasource(validConfig("https://saved.example.test/v1"), http.DefaultClient)
		recorder := httptest.NewRecorder()
		ds.handleTestConnection(recorder, httptest.NewRequest(http.MethodGet, "/test", nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("unexpected method status: %d", recorder.Code)
		}

		recorder = httptest.NewRecorder()
		body := `{"providerKind":"openai","baseUrl":"https://saved.example.test/v1","model":"test-model","unknown":true}`
		ds.handleTestConnection(recorder, httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unexpected invalid request status: %d", recorder.Code)
		}
	})
}

func TestProviderHTTPClientRejectsCrossHostRedirects(t *testing.T) {
	origin := httptest.NewRequest(http.MethodGet, "https://api.example.test/models", nil)
	sameHost := httptest.NewRequest(http.MethodGet, "https://api.example.test/v1/models", nil)
	otherHost := httptest.NewRequest(http.MethodGet, "https://attacker.example.test/models", nil)

	if err := rejectCrossHostRedirect(sameHost, []*http.Request{origin}); err != nil {
		t.Fatalf("same-host redirect should be allowed: %v", err)
	}
	if err := rejectCrossHostRedirect(otherHost, []*http.Request{origin}); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("cross-host redirect should be rejected, got %v", err)
	}
}

func TestGenerateUsesFixedEndpointAndReturnsOnlyProposal(t *testing.T) {
	var received upstreamRequest
	providerBody := `{"choices":[{"message":{"content":"{\"chartType\":\"Bar Chart\",\"xField\":\"region\",\"yField\":\"revenue\",\"rationale\":\"category vs measure\"}"}}],"provider_metadata":{"must":"not leak"}}`
	client := jsonHTTPClient(http.StatusOK, providerBody, func(request *http.Request) {
		if request.URL.String() != "https://api.example.test/v1/chat/completions" {
			t.Errorf("unexpected provider URL: %s", request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("unexpected authorization header")
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
	})

	ds := newDatasource(validConfig("https://api.example.test/v1"), client)
	input := validGenerateRequest()
	input.Prompt = "show revenue by region"
	input.DataHint = "2 fields"
	body := requestBody(t, input)
	recorder := httptest.NewRecorder()
	ds.handleGenerate(recorder, httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(body)))

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected generate response: %d %s", recorder.Code, recorder.Body.String())
	}
	if received.Model != "test-model" || len(received.Messages) != 2 || strings.Contains(recorder.Body.String(), "provider_metadata") {
		t.Fatalf("unexpected provider seam: request=%#v response=%s", received, recorder.Body.String())
	}
	if received.MaxCompletionTokens != maxProviderCompletionTokens || received.MaxTokens != 0 {
		t.Fatalf("expected OpenAI completion limit %d, got %#v", maxProviderCompletionTokens, received)
	}
	if received.Thinking != nil || received.Temperature != nil {
		t.Fatalf("OpenAI request must not receive DeepSeek-only controls: %#v", received)
	}
	if !strings.Contains(received.Messages[1].Content, "frameSummary") || strings.Contains(received.Messages[1].Content, "business-ds") {
		t.Fatalf("provider request should contain frame metadata without datasource identity: %q", received.Messages[1].Content)
	}
	if !strings.Contains(received.Messages[0].Content, "chartInput and specJson to null") ||
		!strings.Contains(received.Messages[0].Content, "semantic_types") ||
		!strings.Contains(received.Messages[0].Content, "Vega-Lite") {
		t.Fatalf("provider prompt does not constrain optional specJson: %q", received.Messages[0].Content)
	}
	var proposal generateChartResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &proposal); err != nil || proposal.ChartType != "Bar Chart" {
		t.Fatalf("unexpected proposal: %#v, %v", proposal, err)
	}
}

func TestChatUsesBoundedConversationAndReturnsOnlyAssistantText(t *testing.T) {
	var received upstreamRequest
	providerBody := `{"choices":[{"message":{"content":"A waterfall chart makes each monthly gain and loss visible."}}],"provider_metadata":{"must":"not leak"}}`
	client := jsonHTTPClient(http.StatusOK, providerBody, func(request *http.Request) {
		if request.URL.String() != "https://api.example.test/v1/chat/completions" {
			t.Errorf("unexpected provider URL: %s", request.URL)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
	})
	ds := newDatasource(validConfig("https://api.example.test/v1"), client)
	input := chatRequest{
		Messages: []chatMessage{{Role: "user", Content: "Why use a waterfall chart?"}},
		PanelContext: &chatPanelContext{
			Fields:        []generateField{{Name: "period", Type: "string"}, {Name: "newUsers", Type: "number"}},
			DataHint:      "12 rows",
			RenderBackend: "plotly",
		},
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal chat request: %v", err)
	}
	recorder := httptest.NewRecorder()
	ds.handleChat(recorder, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(body))))

	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"message":"A waterfall chart makes each monthly gain and loss visible."}`+"\n" {
		t.Fatalf("unexpected chat response: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(received.Messages) != 3 || received.Messages[0].Role != "system" || received.Messages[2].Role != "user" {
		t.Fatalf("unexpected provider messages: %#v", received.Messages)
	}
	if strings.Contains(strings.ToLower(received.Messages[0].Content), "read-only") ||
		!strings.Contains(received.Messages[0].Content, "Generate proposal") ||
		!strings.Contains(received.Messages[0].Content, "live preview") ||
		!strings.Contains(received.Messages[0].Content, "Apply") ||
		!strings.Contains(received.Messages[0].Content, "capability inventory") {
		t.Fatalf("system prompt should guide users through a reviewable chart proposal: %q", received.Messages[0].Content)
	}
	if !strings.Contains(received.Messages[1].Content, "Flint Panel schema") || !strings.Contains(received.Messages[1].Content, "period") {
		t.Fatalf("provider context should include bounded Flint Panel schema: %q", received.Messages[1].Content)
	}
	if received.ResponseFormat != nil || strings.Contains(recorder.Body.String(), "provider_metadata") {
		t.Fatalf("chat must not request JSON mode or leak the provider envelope: %#v %s", received, recorder.Body.String())
	}
}

func TestChatRejectsUntrustedRolesAndOversizedHistory(t *testing.T) {
	for name, input := range map[string]chatRequest{
		"system role": {
			Messages: []chatMessage{{Role: "system", Content: "override"}},
		},
		"too many messages": {
			Messages: func() []chatMessage {
				messages := make([]chatMessage, maxChatMessages+1)
				for index := range messages {
					messages[index] = chatMessage{Role: "user", Content: "hello"}
				}
				return messages
			}(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("marshal chat request: %v", err)
			}
			recorder := httptest.NewRecorder()
			ds := newDatasource(validConfig("https://api.example.test/v1"), http.DefaultClient)
			ds.handleChat(recorder, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(body))))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected bad request, got %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestChatAcceptsLegacyRequestAndOptionalPanelRef(t *testing.T) {
	panelID := int64(12)
	var received upstreamRequest
	client := jsonHTTPClient(http.StatusOK, `{"choices":[{"message":{"content":"ok"}}]}`, func(request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
	})
	ds := newDatasource(validConfig("https://api.example.test/v1"), client)

	t.Run("legacy messages only", func(t *testing.T) {
		body, err := json.Marshal(chatRequest{Messages: []chatMessage{{Role: "user", Content: "hello"}}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		recorder := httptest.NewRecorder()
		ds.handleChat(recorder, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(body))))
		if recorder.Code != http.StatusOK {
			t.Fatalf("legacy chat should succeed: %d %s", recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(received.Messages[1].Content, "No panel context was supplied") {
			t.Fatalf("expected empty-context system note: %q", received.Messages[1].Content)
		}
	})

	t.Run("panelRef whitelist only", func(t *testing.T) {
		body, err := json.Marshal(chatRequest{
			Messages: []chatMessage{{Role: "user", Content: "what is this?"}},
			PanelRef: &chatPanelRef{
				PanelID:      &panelID,
				PluginID:     "timeseries",
				Title:        "CPU",
				TimeFrom:     "now-1h",
				TimeTo:       "now",
				TimeZone:     "browser",
				DashboardUID: "ops",
			},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		recorder := httptest.NewRecorder()
		ds.handleChat(recorder, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(body))))
		if recorder.Code != http.StatusOK {
			t.Fatalf("panelRef chat should succeed: %d %s", recorder.Code, recorder.Body.String())
		}
		contextMsg := received.Messages[1].Content
		if !strings.Contains(contextMsg, "Grafana panel reference") || !strings.Contains(contextMsg, "timeseries") {
			t.Fatalf("provider should receive panelRef: %q", contextMsg)
		}
		if strings.Contains(contextMsg, "targets") || strings.Contains(contextMsg, "scopedVars") {
			t.Fatalf("provider must not receive query/vars payload: %q", contextMsg)
		}
	})
}

func TestChatRejectsInvalidPanelRefAndUnknownFields(t *testing.T) {
	ds := newDatasource(validConfig("https://api.example.test/v1"), http.DefaultClient)

	t.Run("unknown field", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ds.handleChat(recorder, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(
			`{"messages":[{"role":"user","content":"hi"}],"secret":"nope"}`,
		)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected bad request, got %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("oversized panelRef string", func(t *testing.T) {
		body, err := json.Marshal(chatRequest{
			Messages: []chatMessage{{Role: "user", Content: "hi"}},
			PanelRef: &chatPanelRef{Title: strings.Repeat("x", maxPanelRefStringBytes+1)},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		recorder := httptest.NewRecorder()
		ds.handleChat(recorder, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(body))))
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "panelRef") {
			t.Fatalf("expected panelRef validation error: %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("negative panelId", func(t *testing.T) {
		badID := int64(-1)
		body, err := json.Marshal(chatRequest{
			Messages: []chatMessage{{Role: "user", Content: "hi"}},
			PanelRef: &chatPanelRef{PanelID: &badID},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		recorder := httptest.NewRecorder()
		ds.handleChat(recorder, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(body))))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected bad request, got %d %s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestGenerateAcceptsALargerProviderEnvelopeWhenTheProposalIsSmall(t *testing.T) {
	providerBody := `{"choices":[{"message":{"content":"{\"chartType\":\"Bar Chart\",\"xField\":\"region\",\"yField\":\"revenue\"}"}}],"provider_metadata":"` +
		strings.Repeat("x", 300*1024) + `"}`
	ds := newDatasource(
		validConfig("https://api.example.test/v1"),
		jsonHTTPClient(http.StatusOK, providerBody, nil),
	)
	recorder := httptest.NewRecorder()
	ds.handleGenerate(
		recorder,
		httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(requestBody(t, validGenerateRequest()))),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected a small proposal to survive a larger provider envelope: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestGenerateRejectsMisconfiguredAndMalformedProviderResponses(t *testing.T) {
	t.Run("misconfigured instance", func(t *testing.T) {
		ds := newDatasource(providerConfig{}, http.DefaultClient)
		recorder := httptest.NewRecorder()
		ds.handleGenerate(recorder, httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(`{}`)))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("unexpected status: %d", recorder.Code)
		}
	})

	t.Run("malformed provider proposal", func(t *testing.T) {
		providerBody := `{"choices":[{"message":{"content":"not json test-secret"}}]}`
		ds := newDatasource(
			validConfig("https://api.example.test/v1"),
			jsonHTTPClient(http.StatusOK, providerBody, nil),
		)
		recorder := httptest.NewRecorder()
		body := requestBody(t, validGenerateRequest())
		ds.handleGenerate(recorder, httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(body)))
		if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), "test-secret") {
			t.Fatalf("unexpected sanitized error: %d %q", recorder.Code, recorder.Body.String())
		}
	})
}

func TestDecodeGenerateRequestEnforcesBounds(t *testing.T) {
	validInput := validGenerateRequest()
	valid := requestBody(t, validInput)
	if _, err := decodeGenerateRequest(strings.NewReader(valid)); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	validInput.DataHint = strings.Repeat("x", maxDataHintBytes+1)
	oversizedHint := requestBody(t, validInput)
	if _, err := decodeGenerateRequest(strings.NewReader(oversizedHint)); err == nil {
		t.Fatal("expected oversized data hint to be rejected")
	}
	unknown := strings.TrimSuffix(valid, "}") + `,"baseUrl":"https://attacker.invalid"}`
	if _, err := decodeGenerateRequest(strings.NewReader(unknown)); err == nil {
		t.Fatal("expected arbitrary provider URL to be rejected")
	}
}

func TestGenerateNormalizesAliasesAndRejectsValuesOutsideTheCatalog(t *testing.T) {
	t.Run("donut alias", func(t *testing.T) {
		providerBody := `{"choices":[{"message":{"content":"{\"chartType\":\"Donut Chart\",\"xField\":\"region\",\"yField\":\"revenue\"}"}}]}`
		ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusOK, providerBody, nil))
		recorder := httptest.NewRecorder()
		ds.handleGenerate(recorder, httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(requestBody(t, validGenerateRequest()))))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"chartType":"Pie Chart"`) {
			t.Fatalf("expected canonical Pie Chart proposal: %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("unknown chart", func(t *testing.T) {
		providerBody := `{"choices":[{"message":{"content":"{\"chartType\":\"Magic Chart\",\"xField\":\"region\"}"}}]}`
		ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusOK, providerBody, nil))
		recorder := httptest.NewRecorder()
		ds.handleGenerate(recorder, httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(requestBody(t, validGenerateRequest()))))
		if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "invalid chartType") {
			t.Fatalf("expected unknown chart rejection: %d %s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestGenerateRepairsOneInvalidProposal(t *testing.T) {
	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount++
		var received upstreamRequest
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		content := `{"chartType":"Magic Chart","xField":"region"}`
		if requestCount == 2 {
			if len(received.Messages) < 4 || !strings.Contains(received.Messages[len(received.Messages)-1].Content, "invalid chartType") {
				t.Fatalf("repair request does not include the validation error: %#v", received.Messages)
			}
			content = `{"chartType":"Bar Chart","xField":"region","yField":"revenue"}`
		}
		body := `{"choices":[{"message":{"content":` + strconv.Quote(content) + `}}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}

	ds := newDatasource(validConfig("https://api.example.test/v1"), client)
	recorder := httptest.NewRecorder()
	ds.handleGenerate(recorder, httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(requestBody(t, validGenerateRequest()))))

	if recorder.Code != http.StatusOK || requestCount != 2 || !strings.Contains(recorder.Body.String(), `"chartType":"Bar Chart"`) {
		t.Fatalf("unexpected repaired proposal: status=%d requests=%d body=%s", recorder.Code, requestCount, recorder.Body.String())
	}
}

func TestRepairEndpointReturnsOneStructuredCorrection(t *testing.T) {
	var received upstreamRequest
	corrected := `{"chartType":"Bar Chart","xField":"region","yField":"revenue","chartInput":{"semantic_types":{"region":"Category","revenue":"Amount"},"chart_spec":{"chartType":"Bar Chart","encodings":{"x":{"field":"region"},"y":{"field":"revenue"}},"chartProperties":{"cornerRadius":5}}}}`
	providerBody := `{"choices":[{"message":{"content":` + strconv.Quote(corrected) + `}}]}`
	ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusOK, providerBody, func(request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode repair provider request: %v", err)
		}
	}))
	repair := repairChartRequest{
		Request: validGenerateRequest(),
		Candidate: generateChartResponse{
			ChartType: "Bar Chart",
			XField:    "region",
			YField:    "revenue",
			ChartInput: map[string]any{
				"semantic_types": map[string]any{"region": "Category", "revenue": "Amount"},
				"chart_spec": map[string]any{
					"chartType":       "Bar Chart",
					"encodings":       map[string]any{"x": map[string]any{"field": "region"}, "y": map[string]any{"field": "revenue"}},
					"chartProperties": map[string]any{"cornerRadius": float64(99)},
				},
			},
		},
		CompileError: "chartProperties.cornerRadius must be between 0 and 15",
		Attempt:      1,
	}
	body, err := json.Marshal(repair)
	if err != nil {
		t.Fatalf("marshal repair request: %v", err)
	}
	recorder := httptest.NewRecorder()
	ds.handleRepair(recorder, httptest.NewRequest(http.MethodPost, "/repair", strings.NewReader(string(body))))

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"chartInput"`) || !strings.Contains(recorder.Body.String(), `"cornerRadius":5`) {
		t.Fatalf("unexpected repair response: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(received.Messages) != 2 || !strings.Contains(received.Messages[1].Content, repair.CompileError) {
		t.Fatalf("provider did not receive exact compiler evidence: %#v", received.Messages)
	}
}

func TestRepairEndpointEnforcesOneBoundedAttempt(t *testing.T) {
	ds := newDatasource(validConfig("https://api.example.test/v1"), http.DefaultClient)
	input := repairChartRequest{
		Request:      validGenerateRequest(),
		Candidate:    generateChartResponse{ChartType: "Bar Chart", XField: "region", YField: "revenue"},
		CompileError: "compile failed",
		Attempt:      2,
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal repair request: %v", err)
	}
	recorder := httptest.NewRecorder()
	ds.handleRepair(recorder, httptest.NewRequest(http.MethodPost, "/repair", strings.NewReader(string(body))))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected bounded repair rejection, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestStructuredChartInputRejectsEmbeddedDataAndUnknownFields(t *testing.T) {
	input := validGenerateRequest()
	for name, chartInput := range map[string]map[string]any{
		"embedded data": {
			"data":       map[string]any{"values": []any{map[string]any{"region": "North"}}},
			"chart_spec": map[string]any{"chartType": "Bar Chart", "encodings": map[string]any{}},
		},
		"unknown field": {
			"chart_spec": map[string]any{
				"chartType": "Bar Chart",
				"encodings": map[string]any{"x": map[string]any{"field": "country"}, "y": map[string]any{"field": "revenue"}},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			proposal := generateChartResponse{ChartType: "Bar Chart", XField: "region", YField: "revenue", ChartInput: chartInput}
			if err := validateProposal(&proposal, input); err == nil {
				t.Fatal("expected unsafe structured chart input to be rejected")
			}
		})
	}
}

func TestGenerateReturnsSanitizedUpstreamErrorMessage(t *testing.T) {
	body := `{"error":{"message":"invalid model test-secret"}}`
	ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusBadRequest, body, nil))
	recorder := httptest.NewRecorder()
	ds.handleGenerate(recorder, httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(requestBody(t, validGenerateRequest()))))
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "invalid model <redacted>") || strings.Contains(recorder.Body.String(), "test-secret") {
		t.Fatalf("unexpected upstream error: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestModelsListAllowsMissingModelAndUsesProviderEndpoints(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		config := validConfig("https://api.openai.com/v1")
		config.model = ""
		var gotURL string
		var gotAuth string
		ds := newDatasource(config, jsonHTTPClient(http.StatusOK, `{"object":"list","data":[{"id":"gpt-b"},{"id":"gpt-a"},{"id":""},{"owned_by":"x"},{"id":"`+strings.Repeat("m", 257)+`"}]}`, func(request *http.Request) {
			gotURL = request.URL.String()
			gotAuth = request.Header.Get("Authorization")
			if request.Method != http.MethodGet {
				t.Fatalf("expected GET, got %s", request.Method)
			}
		}))
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d %s", recorder.Code, recorder.Body.String())
		}
		if gotURL != "https://api.openai.com/v1/models" || gotAuth != "Bearer test-secret" {
			t.Fatalf("unexpected upstream request: url=%q auth=%q", gotURL, gotAuth)
		}
		if recorder.Body.String() != "{\"models\":[\"gpt-a\",\"gpt-b\"]}\n" {
			t.Fatalf("unexpected models body: %q", recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "test-secret") {
			t.Fatal("api key leaked in models response")
		}
	})

	t.Run("deepseek strips /v1 for the models endpoint", func(t *testing.T) {
		config := validConfig("https://api.deepseek.com/v1")
		config.providerKind = providerKindDeepSeek
		config.model = ""
		var gotURL string
		ds := newDatasource(config, jsonHTTPClient(http.StatusOK, `{"object":"list","data":[{"id":"deepseek-chat","object":"model","owned_by":"deepseek"}]}`, func(request *http.Request) {
			gotURL = request.URL.String()
		}))
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
		if recorder.Code != http.StatusOK || gotURL != "https://api.deepseek.com/models" {
			t.Fatalf("unexpected deepseek models call: status=%d url=%q body=%s", recorder.Code, gotURL, recorder.Body.String())
		}
	})

	t.Run("deepseek root base url", func(t *testing.T) {
		config := validConfig("https://api.deepseek.com")
		config.providerKind = providerKindDeepSeek
		var gotURL string
		ds := newDatasource(config, jsonHTTPClient(http.StatusOK, `{"data":[{"id":"deepseek-reasoner"}]}`, func(request *http.Request) {
			gotURL = request.URL.String()
		}))
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
		if recorder.Code != http.StatusOK || gotURL != "https://api.deepseek.com/models" {
			t.Fatalf("unexpected deepseek root models call: status=%d url=%q", recorder.Code, gotURL)
		}
	})

	t.Run("chat still requires model", func(t *testing.T) {
		config := validConfig("https://api.example.test/v1")
		config.model = ""
		ds := newDatasource(config, http.DefaultClient)
		recorder := httptest.NewRecorder()
		ds.handleChat(recorder, httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected chat to reject empty model, got %d", recorder.Code)
		}
	})
}

func TestModelsListRejectsMisconfiguredAndSanitizesProviderErrors(t *testing.T) {
	t.Run("method not allowed", func(t *testing.T) {
		ds := newDatasource(validConfig("https://api.example.test/v1"), http.DefaultClient)
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodPost, "/models", strings.NewReader(`{"apiKey":"leak"}`)))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("unexpected status: %d", recorder.Code)
		}
	})

	t.Run("missing api key", func(t *testing.T) {
		config := validConfig("https://api.example.test/v1")
		config.apiKey = ""
		config.model = ""
		ds := newDatasource(config, http.DefaultClient)
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("unexpected status: %d", recorder.Code)
		}
	})

	t.Run("auth failure redacts secret", func(t *testing.T) {
		ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusUnauthorized, `{"error":{"message":"bad key test-secret"}}`, nil))
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
		if recorder.Code != http.StatusUnauthorized || strings.Contains(recorder.Body.String(), "test-secret") || !strings.Contains(recorder.Body.String(), "<redacted>") {
			t.Fatalf("unexpected auth error: %d %s", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "enter a model ID manually") {
			t.Fatalf("auth failure should not be framed as a manual-id fallback: %s", recorder.Body.String())
		}
	})

	t.Run("unsupported endpoint suggests manual model id", func(t *testing.T) {
		ds := newDatasource(validConfig("https://gateway.example.test/custom"), jsonHTTPClient(http.StatusNotFound, `{"error":{"message":"no models route"}}`, nil))
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
		if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "enter a model ID manually") || strings.Contains(strings.ToLower(recorder.Body.String()), "api key") {
			t.Fatalf("unexpected unsupported endpoint error: %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("malformed catalog", func(t *testing.T) {
		ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusOK, `{"data":"nope"}`, nil))
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
		if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "enter a model ID manually") {
			t.Fatalf("unexpected status: %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("oversized catalog", func(t *testing.T) {
		huge := `{"data":[` + strings.Repeat(`{"id":"m"},`, 100) + `{"id":"z"}]}` + strings.Repeat("x", maxProviderResponseBytes)
		ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusOK, huge, nil))
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
		if recorder.Code != http.StatusBadGateway {
			t.Fatalf("unexpected status: %d", recorder.Code)
		}
	})

	t.Run("rate limit", func(t *testing.T) {
		ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, nil))
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
		if recorder.Code != http.StatusTooManyRequests {
			t.Fatalf("unexpected status: %d", recorder.Code)
		}
	})

	t.Run("ignores apiKey query parameter", func(t *testing.T) {
		var gotAuth string
		ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusOK, `{"data":[{"id":"a"}]}`, func(request *http.Request) {
			gotAuth = request.Header.Get("Authorization")
		}))
		recorder := httptest.NewRecorder()
		ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models?apiKey=from-browser", nil))
		if recorder.Code != http.StatusOK || gotAuth != "Bearer test-secret" {
			t.Fatalf("query apiKey must be ignored: status=%d auth=%q body=%s", recorder.Code, gotAuth, recorder.Body.String())
		}
	})
}

func TestModelsListURLNormalization(t *testing.T) {
	cases := []struct {
		name         string
		baseURL      string
		providerKind string
		want         string
		wantErr      bool
	}{
		{name: "openai v1", baseURL: "https://api.openai.com/v1", providerKind: providerKindOpenAI, want: "https://api.openai.com/v1/models"},
		{name: "openai trailing slash trimmed by parse", baseURL: "https://api.openai.com/v1", providerKind: providerKindOpenAI, want: "https://api.openai.com/v1/models"},
		{name: "deepseek v1", baseURL: "https://api.deepseek.com/v1", providerKind: providerKindDeepSeek, want: "https://api.deepseek.com/models"},
		{name: "deepseek official root", baseURL: "https://api.deepseek.com", providerKind: providerKindDeepSeek, want: "https://api.deepseek.com/models"},
		{name: "custom openai gateway", baseURL: "https://gateway.example.test/v1", providerKind: providerKindOpenAI, want: "https://gateway.example.test/v1/models"},
		{name: "rejects credentials", baseURL: "https://user:pass@api.example.test/v1", providerKind: providerKindOpenAI, wantErr: true},
		{name: "rejects query", baseURL: "https://api.example.test/v1?x=1", providerKind: providerKindOpenAI, wantErr: true},
		{name: "rejects fragment", baseURL: "https://api.example.test/v1#frag", providerKind: providerKindOpenAI, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := providerConfig{baseURL: tc.baseURL, providerKind: tc.providerKind, apiKey: "test-secret"}
			got, err := modelsListURL(config)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("modelsListURL() = %q, %v; want %q", got, err, tc.want)
			}
			parsedBase, err := url.Parse(tc.baseURL)
			if err != nil {
				t.Fatalf("parse base: %v", err)
			}
			parsedModels, err := url.Parse(got)
			if err != nil {
				t.Fatalf("parse models: %v", err)
			}
			if parsedModels.Host != parsedBase.Host || parsedModels.User != nil || parsedModels.RawQuery != "" || parsedModels.Fragment != "" {
				t.Fatalf("models URL escaped host or retained unsafe parts: %#v", parsedModels)
			}
		})
	}
}

func TestModelsListNeverHitsChatCompletions(t *testing.T) {
	var paths []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		if request.URL.Path != "/v1/models" {
			t.Fatalf("models list must not call %s", request.URL.String())
		}
		if request.Method != http.MethodGet {
			t.Fatalf("expected GET models, got %s", request.Method)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"object":"list","data":[{"id":"stub-chat"}]}`)),
			Request:    request,
		}, nil
	})}

	ds := newDatasource(validConfig("https://stub.example.test/v1"), client)
	recorder := httptest.NewRecorder()
	ds.handleModels(recorder, httptest.NewRequest(http.MethodGet, "/models", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(paths) != 1 || paths[0] != "/v1/models" {
		t.Fatalf("unexpected upstream paths: %#v", paths)
	}
}

func TestLLMProviderHonorsHTTPClientTimeout(t *testing.T) {
	client := &http.Client{
		Timeout: 5 * time.Millisecond,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
	}
	provider, err := newLLMProvider(validConfig("https://api.example.test/v1"), client)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	started := time.Now()
	_, err = provider.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected timed out models request to fail")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("provider ignored the HTTP client timeout: %s", elapsed)
	}
	if providerCallStatus(err) != http.StatusBadGateway || !strings.Contains(err.Error(), "Could not reach") {
		t.Fatalf("unexpected timeout error: status=%d error=%v", providerCallStatus(err), err)
	}
}

func TestResolveAPIKeyUsesOnlyTheSelectedProviderSecret(t *testing.T) {
	deepseek, err := parseProviderConfig(backend.DataSourceInstanceSettings{
		JSONData: []byte(`{"baseUrl":"https://api.deepseek.com/v1","model":"deepseek-chat","providerKind":"deepseek"}`),
		DecryptedSecureJSONData: map[string]string{
			"deepseekApiKey": " deepseek-secret ",
			"openaiApiKey":   "openai-secret",
		},
	})
	if err != nil {
		t.Fatalf("parse deepseek config: %v", err)
	}
	if deepseek.apiKey != "deepseek-secret" {
		t.Fatalf("expected deepseek-specific key, got %q", deepseek.apiKey)
	}

	openai, err := parseProviderConfig(backend.DataSourceInstanceSettings{
		JSONData: []byte(`{"baseUrl":"https://api.openai.com/v1","model":"gpt-5","providerKind":"openai"}`),
		DecryptedSecureJSONData: map[string]string{
			"openaiApiKey": "openai-secret",
		},
	})
	if err != nil {
		t.Fatalf("parse openai config: %v", err)
	}
	if openai.apiKey != "openai-secret" {
		t.Fatalf("expected openai-specific key, got %q", openai.apiKey)
	}

	withoutSelectedProviderKey, err := parseProviderConfig(backend.DataSourceInstanceSettings{
		JSONData:                []byte(`{"baseUrl":"https://api.openai.com/v1","model":"gpt-5","providerKind":"openai"}`),
		DecryptedSecureJSONData: map[string]string{"apiKey": "generic-key", "deepseekApiKey": "deepseek-secret"},
	})
	if err != nil {
		t.Fatalf("parse config without selected provider key: %v", err)
	}
	if withoutSelectedProviderKey.apiKey != "" {
		t.Fatalf("generic or other-provider keys must not be used, got %q", withoutSelectedProviderKey.apiKey)
	}
	if err := withoutSelectedProviderKey.validateConnection(); err == nil {
		t.Fatal("configuration without the selected provider key should fail validation")
	}

	for _, providerKind := range []string{"", "openai-compatible", "gemini"} {
		config, err := parseProviderConfig(backend.DataSourceInstanceSettings{
			JSONData:                []byte(`{"baseUrl":"https://api.openai.com/v1","model":"gpt-5","providerKind":"` + providerKind + `"}`),
			DecryptedSecureJSONData: map[string]string{"openaiApiKey": "openai-secret"},
		})
		if err != nil {
			t.Fatalf("parse config for %q: %v", providerKind, err)
		}
		if err := config.validateConnection(); err == nil {
			t.Fatalf("provider kind %q should fail validation", providerKind)
		}
	}
}
