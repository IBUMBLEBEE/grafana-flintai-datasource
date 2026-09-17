package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
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
			{ChartType: "Bar Chart", Channels: []string{"x", "y", "color"}},
			{ChartType: "Pie Chart", Channels: []string{"size", "color"}},
		},
		FrameSummary: []frameSummary{{FrameIndex: 0, RefID: "A", Fields: []string{"region", "revenue"}}},
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
		JSONData:                []byte(`{"baseUrl":"https://api.example.test/v1/","model":" test-model ","providerKind":"openai-compatible"}`),
		DecryptedSecureJSONData: map[string]string{"apiKey": " test-secret "},
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
	if !strings.Contains(received.Messages[0].Content, "null for specJson") || !strings.Contains(received.Messages[0].Content, "Vega-Lite") {
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
	if !strings.Contains(received.Messages[0].Content, "read-only conversational assistant") {
		t.Fatalf("system prompt should describe a read-only Grafana panel assistant: %q", received.Messages[0].Content)
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

func TestGenerateReturnsSanitizedUpstreamErrorMessage(t *testing.T) {
	body := `{"error":{"message":"invalid model test-secret"}}`
	ds := newDatasource(validConfig("https://api.example.test/v1"), jsonHTTPClient(http.StatusBadRequest, body, nil))
	recorder := httptest.NewRecorder()
	ds.handleGenerate(recorder, httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(requestBody(t, validGenerateRequest()))))
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "invalid model <redacted>") || strings.Contains(recorder.Body.String(), "test-secret") {
		t.Fatalf("unexpected upstream error: %d %s", recorder.Code, recorder.Body.String())
	}
}
