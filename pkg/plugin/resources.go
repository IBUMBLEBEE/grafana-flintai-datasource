package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const (
	maxRequestBytes      = 128 * 1024
	maxPromptBytes       = 4 * 1024
	maxDataHintBytes     = 4 * 1024
	maxSampleRowsBytes   = 4 * 1024
	maxCompileErrorBytes = 4 * 1024
	maxSampleRows        = 12
	maxChatMessages      = 10
	maxChatBytes         = 16 * 1024
	// Some OpenAI-compatible providers include large reasoning or metadata envelopes.
	// The extracted Flint proposal remains capped separately at maxProposalBytes.
	maxProviderResponseBytes    = 1024 * 1024
	maxProposalBytes            = 64 * 1024
	maxChatResponseBytes        = 64 * 1024
	maxProviderTestTokens       = 16
	maxFields                   = 128
	maxProviderCompletionTokens = 4096
	maxPanelRefStringBytes      = 256
	maxPanelID                  = 1_000_000_000
	maxModelIDBytes             = 256
	maxModelCatalogEntries      = 2048
	maxBaseURLBytes             = 2048
	maxAPIKeyBytes              = 16 * 1024
	allowHTTPEnv                = "FLINT_AI_ALLOW_LOCAL_HTTP"
)

type providerJSON struct {
	BaseURL      string `json:"baseUrl"`
	Model        string `json:"model"`
	ProviderKind string `json:"providerKind"`
}

type providerConfig struct {
	baseURL      string
	model        string
	providerKind string
	apiKey       string
}

func parseProviderConfig(settings backend.DataSourceInstanceSettings) (providerConfig, error) {
	var raw providerJSON
	if len(settings.JSONData) > 0 {
		if err := json.Unmarshal(settings.JSONData, &raw); err != nil {
			return providerConfig{}, fmt.Errorf("invalid Flint AI jsonData: %w", err)
		}
	}
	return providerConfig{
		baseURL:      strings.TrimRight(strings.TrimSpace(raw.BaseURL), "/"),
		model:        strings.TrimSpace(raw.Model),
		providerKind: strings.TrimSpace(raw.ProviderKind),
		apiKey:       resolveAPIKey(settings.DecryptedSecureJSONData, strings.TrimSpace(raw.ProviderKind)),
	}, nil
}

func resolveAPIKey(decrypted map[string]string, providerKind string) string {
	switch providerKind {
	case providerKindDeepSeek:
		return strings.TrimSpace(decrypted["deepseekApiKey"])
	case providerKindOpenAI:
		return strings.TrimSpace(decrypted["openaiApiKey"])
	default:
		return ""
	}
}

func (config providerConfig) validateConnection() error {
	parsed, err := url.Parse(config.baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("Flint AI Base URL must be an absolute URL without credentials, query, or fragment")
	}
	if strings.ContainsAny(parsed.Path, "?#") || strings.ContainsAny(config.baseURL, "?#") {
		return errors.New("Flint AI Base URL must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" {
		if parsed.Scheme != "http" || os.Getenv(allowHTTPEnv) != "true" || !isLocalDevelopmentHost(parsed.Hostname()) {
			return errors.New("Flint AI Base URL must use HTTPS (local HTTP requires explicit backend development opt-in)")
		}
	}
	if err := validateLiteralProviderHost(parsed.Hostname()); err != nil {
		return err
	}
	if _, err := adapterForProvider(config.providerKind); err != nil {
		return err
	}
	if config.apiKey == "" {
		return errors.New("Flint AI API key is not configured")
	}
	return nil
}

func (config providerConfig) validate() error {
	if err := config.validateConnection(); err != nil {
		return err
	}
	if config.model == "" || len(config.model) > maxModelIDBytes {
		return errors.New("Flint AI model is not configured or is too long")
	}
	return nil
}

func isLocalDevelopmentHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "host.docker.internal") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host != "" && !strings.Contains(host, ".") && !strings.Contains(host, ":")
}

type generateField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type chartCatalogEntry struct {
	ChartType        string                 `json:"chartType"`
	Channels         []string               `json:"channels"`
	RequiredChannels []string               `json:"requiredChannels"`
	Properties       []chartPropertyCatalog `json:"properties"`
}

type chartPropertyCatalog struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Options []any    `json:"options,omitempty"`
}

type frameSummary struct {
	FrameIndex int      `json:"frameIndex"`
	RefID      string   `json:"refId,omitempty"`
	Fields     []string `json:"fields"`
}

type generateChartRequest struct {
	Prompt             string              `json:"prompt"`
	Fields             []generateField     `json:"fields"`
	DataHint           string              `json:"dataHint,omitempty"`
	SuggestedChartType string              `json:"suggestedChartType,omitempty"`
	RenderBackend      string              `json:"renderBackend"`
	ChartCatalog       []chartCatalogEntry `json:"chartCatalog"`
	SemanticTypes      []string            `json:"semanticTypes"`
	SampleRows         []map[string]any    `json:"sampleRows,omitempty"`
	FrameSummary       []frameSummary      `json:"frameSummary,omitempty"`
	Conversation       []chatMessage       `json:"conversation,omitempty"`
}

type generateChartResponse struct {
	ChartType  string         `json:"chartType"`
	XField     string         `json:"xField,omitempty"`
	YField     string         `json:"yField,omitempty"`
	ColorField string         `json:"colorField,omitempty"`
	ChartInput map[string]any `json:"chartInput,omitempty"`
	SpecJSON   string         `json:"specJson,omitempty"`
	Rationale  string         `json:"rationale,omitempty"`
}

type repairChartRequest struct {
	Request      generateChartRequest  `json:"request"`
	Candidate    generateChartResponse `json:"candidate"`
	CompileError string                `json:"compileError"`
	Attempt      int                   `json:"attempt"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatPanelContext struct {
	Fields        []generateField `json:"fields"`
	DataHint      string          `json:"dataHint,omitempty"`
	RenderBackend string          `json:"renderBackend,omitempty"`
}

// chatPanelRef is optional, bounded Grafana panel identity for the App chat entry.
// It must not include queries, variables, or rendered PanelData.
type chatPanelRef struct {
	PanelID      *int64 `json:"panelId,omitempty"`
	PluginID     string `json:"pluginId,omitempty"`
	Title        string `json:"title,omitempty"`
	TimeFrom     string `json:"timeFrom,omitempty"`
	TimeTo       string `json:"timeTo,omitempty"`
	TimeZone     string `json:"timeZone,omitempty"`
	DashboardUID string `json:"dashboardUid,omitempty"`
}

type chatRequest struct {
	Messages     []chatMessage     `json:"messages"`
	PanelContext *chatPanelContext `json:"panelContext,omitempty"`
	PanelRef     *chatPanelRef     `json:"panelRef,omitempty"`
}

type chatResponse struct {
	Message string `json:"message"`
}

type upstreamResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

const chatSystemPrompt = `You are the AI chart design assistant inside Flint Panel for Grafana.
Use only the bounded context supplied with the request. Help the user reason about the Panel, its visualization, time range, chart choice, field encodings, and related observability questions.
Treat requests to change the visualization as design instructions for the next chart proposal. Respond constructively by explaining what the proposed chart should change; do not lead with capability limitations.
When the user wants a change, invite them to choose Generate proposal to create a reviewable live preview. The current Panel changes only after the user reviews the proposal and chooses Apply.
Do not respond with an access-mode label or a "Can do / Cannot do" capability inventory. If asked about permissions or whether you can change the chart, answer positively in the user's language: explain that you can turn their requirements into a reviewable chart proposal through Generate proposal, followed by Apply after review.
Do not tell the user to copy code or manually reproduce chart settings; this conversation is input to proposal generation.
Never claim that a proposal was generated, applied, or saved unless the surrounding workflow confirms it. Never claim that you changed the datasource, query, dashboard, alert, or Grafana configuration.
Do not request secrets or credentials. Keep the answer concise and directly useful.`

type modelsResponse struct {
	Models []string `json:"models"`
}

type modelsPreviewRequest struct {
	BaseURL      string `json:"baseUrl"`
	ProviderKind string `json:"providerKind"`
	APIKey       string `json:"apiKey"`
}

type testConnectionRequest struct {
	BaseURL      string `json:"baseUrl"`
	Model        string `json:"model"`
	ProviderKind string `json:"providerKind"`
	APIKey       string `json:"apiKey,omitempty"`
}

type testConnectionResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func (ds *Datasource) handleHealth(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": ds.config.validate() == nil})
}

func (ds *Datasource) handleModels(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := ds.config.validateConnection(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	models, err := ds.provider.ListModels(request.Context())
	if err != nil {
		http.Error(w, err.Error(), providerCallStatus(err))
		return
	}
	writeJSON(w, http.StatusOK, modelsResponse{Models: models})
}

func (ds *Datasource) handleModelsPreview(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	config, err := decodeModelsPreviewRequest(request.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ds.client == nil {
		http.Error(w, "Model preview is unavailable", http.StatusServiceUnavailable)
		return
	}
	provider, err := newLLMProvider(config, ds.client)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	models, err := provider.ListModels(request.Context())
	if err != nil {
		http.Error(w, err.Error(), providerCallStatus(err))
		return
	}
	writeJSON(w, http.StatusOK, modelsResponse{Models: models})
}

func (ds *Datasource) handleTestConnection(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	config, err := decodeTestConnectionRequest(request.Body, ds.config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ds.client == nil {
		http.Error(w, "AI connection test is unavailable", http.StatusServiceUnavailable)
		return
	}
	provider, err := newLLMProvider(config, ds.client)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := provider.TestConnection(request.Context()); err != nil {
		http.Error(w, err.Error(), providerCallStatus(err))
		return
	}
	writeJSON(w, http.StatusOK, testConnectionResponse{OK: true, Message: "AI connection succeeded"})
}

func (ds *Datasource) handleGenerate(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := ds.config.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	input, err := decodeGenerateRequest(request.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	proposal, err := ds.provider.Generate(request.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, proposal)
}

func (ds *Datasource) handleRepair(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := ds.config.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	input, err := decodeRepairRequest(request.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	proposal, err := ds.provider.Repair(request.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, proposal)
}

func (ds *Datasource) handleChat(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := ds.config.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	input, err := decodeChatRequest(request.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	message, err := ds.provider.Chat(request.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, chatResponse{Message: message})
}

func decodeGenerateRequest(body io.Reader) (generateChartRequest, error) {
	var input generateChartRequest
	decoder := json.NewDecoder(io.LimitReader(body, maxRequestBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, errors.New("invalid generate request")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return input, errors.New("invalid generate request")
	}
	if err := validateGenerateRequest(&input); err != nil {
		return input, err
	}
	return input, nil
}

func validateGenerateRequest(input *generateChartRequest) error {
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" || len(input.Prompt) > maxPromptBytes || !utf8.ValidString(input.Prompt) {
		return fmt.Errorf("prompt must be valid UTF-8 between 1 and %d bytes", maxPromptBytes)
	}
	if len(input.Fields) == 0 || len(input.Fields) > maxFields {
		return fmt.Errorf("fields must contain between 1 and %d entries", maxFields)
	}
	seen := make(map[string]struct{}, len(input.Fields))
	for i := range input.Fields {
		input.Fields[i].Name = strings.TrimSpace(input.Fields[i].Name)
		input.Fields[i].Type = strings.TrimSpace(input.Fields[i].Type)
		field := input.Fields[i]
		if field.Name == "" || field.Type == "" || len(field.Name) > 256 || len(field.Type) > 64 {
			return errors.New("fields contain an invalid name or type")
		}
		if _, exists := seen[field.Name]; exists {
			return errors.New("fields contain a duplicate name")
		}
		seen[field.Name] = struct{}{}
	}
	if len(input.DataHint) > maxDataHintBytes || !utf8.ValidString(input.DataHint) {
		return fmt.Errorf("dataHint exceeds the %d-byte limit or is not valid UTF-8", maxDataHintBytes)
	}
	if len(input.SuggestedChartType) > 128 {
		return errors.New("suggestedChartType is too long")
	}
	if input.RenderBackend != "echarts" && input.RenderBackend != "vegalite" && input.RenderBackend != "plotly" && input.RenderBackend != "chartjs" {
		return errors.New("renderBackend is invalid")
	}
	if len(input.ChartCatalog) == 0 || len(input.ChartCatalog) > 64 {
		return errors.New("chartCatalog must contain between 1 and 64 entries")
	}
	seenCharts := make(map[string]struct{}, len(input.ChartCatalog))
	for _, entry := range input.ChartCatalog {
		if strings.TrimSpace(entry.ChartType) == "" || len(entry.ChartType) > 128 || len(entry.Channels) > 24 {
			return errors.New("chartCatalog contains an invalid entry")
		}
		if _, exists := seenCharts[entry.ChartType]; exists {
			return errors.New("chartCatalog contains a duplicate chartType")
		}
		seenCharts[entry.ChartType] = struct{}{}
		for _, channel := range entry.Channels {
			if strings.TrimSpace(channel) == "" || len(channel) > 64 {
				return errors.New("chartCatalog contains an invalid channel")
			}
		}
		if len(entry.RequiredChannels) > len(entry.Channels) || len(entry.Properties) > 64 {
			return errors.New("chartCatalog contains invalid requirements or properties")
		}
		channelSet := make(map[string]struct{}, len(entry.Channels))
		for _, channel := range entry.Channels {
			channelSet[channel] = struct{}{}
		}
		for _, channel := range entry.RequiredChannels {
			if _, ok := channelSet[channel]; !ok {
				return errors.New("chartCatalog requires an unavailable channel")
			}
		}
		seenProperties := make(map[string]struct{}, len(entry.Properties))
		for _, property := range entry.Properties {
			if strings.TrimSpace(property.Key) == "" || len(property.Key) > 128 ||
				(property.Type != "continuous" && property.Type != "discrete" && property.Type != "binary") {
				return errors.New("chartCatalog contains an invalid property")
			}
			if _, ok := seenProperties[property.Key]; ok {
				return errors.New("chartCatalog contains a duplicate property")
			}
			seenProperties[property.Key] = struct{}{}
			if property.Type == "continuous" && (property.Min == nil || property.Max == nil || *property.Min > *property.Max) {
				return errors.New("chartCatalog contains an invalid continuous property")
			}
			if property.Type == "discrete" && len(property.Options) == 0 {
				return errors.New("chartCatalog contains an invalid discrete property")
			}
		}
	}
	if len(input.SemanticTypes) == 0 || len(input.SemanticTypes) > 128 {
		return errors.New("semanticTypes must contain between 1 and 128 entries")
	}
	seenSemanticTypes := make(map[string]struct{}, len(input.SemanticTypes))
	for _, semanticType := range input.SemanticTypes {
		if strings.TrimSpace(semanticType) == "" || len(semanticType) > 64 {
			return errors.New("semanticTypes contains an invalid entry")
		}
		if _, ok := seenSemanticTypes[semanticType]; ok {
			return errors.New("semanticTypes contains a duplicate entry")
		}
		seenSemanticTypes[semanticType] = struct{}{}
	}
	if len(input.SampleRows) > maxSampleRows {
		return errors.New("sampleRows contains too many rows")
	}
	encodedRows, err := json.Marshal(input.SampleRows)
	if err != nil || len(encodedRows) > maxSampleRowsBytes {
		return errors.New("sampleRows exceeds the size limit")
	}
	for _, row := range input.SampleRows {
		for key, value := range row {
			if _, ok := seen[key]; !ok {
				return errors.New("sampleRows references an unknown field")
			}
			switch value.(type) {
			case nil, string, bool, float64:
			default:
				return errors.New("sampleRows values must be scalar")
			}
		}
	}
	if len(input.FrameSummary) > 64 {
		return errors.New("frameSummary contains too many entries")
	}
	for _, frame := range input.FrameSummary {
		if frame.FrameIndex < 0 || len(frame.RefID) > 128 || len(frame.Fields) > maxFields {
			return errors.New("frameSummary contains an invalid frame")
		}
		for _, field := range frame.Fields {
			if strings.TrimSpace(field) == "" || len(field) > 256 {
				return errors.New("frameSummary contains an invalid field")
			}
		}
	}
	if err := validateConversation(input.Conversation, maxChatMessages, maxChatBytes, true); err != nil {
		return fmt.Errorf("conversation %w", err)
	}
	return nil
}

func decodeRepairRequest(body io.Reader) (repairChartRequest, error) {
	var input repairChartRequest
	decoder := json.NewDecoder(io.LimitReader(body, maxRequestBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || ensureJSONEOF(decoder) != nil {
		return input, errors.New("invalid repair request")
	}
	input.CompileError = strings.TrimSpace(input.CompileError)
	if input.Attempt != 1 || input.CompileError == "" || len(input.CompileError) > maxCompileErrorBytes || !utf8.ValidString(input.CompileError) {
		return input, errors.New("repair request contains an invalid compile error or attempt")
	}
	if err := validateGenerateRequest(&input.Request); err != nil {
		return input, fmt.Errorf("repair request context: %w", err)
	}
	if err := validateProposal(&input.Candidate, input.Request); err != nil {
		return input, fmt.Errorf("repair request candidate: %w", err)
	}
	return input, nil
}

func decodeModelsPreviewRequest(body io.Reader) (providerConfig, error) {
	var input modelsPreviewRequest
	decoder := json.NewDecoder(io.LimitReader(body, maxRequestBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || ensureJSONEOF(decoder) != nil {
		return providerConfig{}, errors.New("invalid models preview request")
	}
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.ProviderKind = strings.TrimSpace(input.ProviderKind)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if len(input.BaseURL) > maxBaseURLBytes || len(input.APIKey) > maxAPIKeyBytes ||
		!utf8.ValidString(input.BaseURL) || !utf8.ValidString(input.APIKey) {
		return providerConfig{}, errors.New("invalid models preview request")
	}
	config := providerConfig{
		baseURL:      input.BaseURL,
		providerKind: input.ProviderKind,
		apiKey:       input.APIKey,
	}
	if err := config.validateConnection(); err != nil {
		return providerConfig{}, err
	}
	return config, nil
}

func decodeTestConnectionRequest(body io.Reader, saved providerConfig) (providerConfig, error) {
	var input testConnectionRequest
	decoder := json.NewDecoder(io.LimitReader(body, maxRequestBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || ensureJSONEOF(decoder) != nil {
		return providerConfig{}, errors.New("invalid AI connection test request")
	}
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.Model = strings.TrimSpace(input.Model)
	input.ProviderKind = strings.TrimSpace(input.ProviderKind)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if len(input.BaseURL) > maxBaseURLBytes || len(input.Model) > maxModelIDBytes || len(input.APIKey) > maxAPIKeyBytes ||
		!utf8.ValidString(input.BaseURL) || !utf8.ValidString(input.Model) || !utf8.ValidString(input.APIKey) {
		return providerConfig{}, errors.New("invalid AI connection test request")
	}
	if input.APIKey == "" && input.ProviderKind == saved.providerKind && input.BaseURL == saved.baseURL {
		input.APIKey = saved.apiKey
	}
	config := providerConfig{
		baseURL:      input.BaseURL,
		model:        input.Model,
		providerKind: input.ProviderKind,
		apiKey:       input.APIKey,
	}
	if err := config.validate(); err != nil {
		return providerConfig{}, err
	}
	return config, nil
}

func decodeChatRequest(body io.Reader) (chatRequest, error) {
	var input chatRequest
	decoder := json.NewDecoder(io.LimitReader(body, maxRequestBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || ensureJSONEOF(decoder) != nil {
		return input, errors.New("invalid chat request")
	}
	if err := validateConversation(input.Messages, maxChatMessages, maxChatBytes, false); err != nil {
		return input, fmt.Errorf("messages %w", err)
	}
	if input.PanelContext != nil {
		if len(input.PanelContext.Fields) > maxFields {
			return input, errors.New("panelContext contains too many fields")
		}
		for _, field := range input.PanelContext.Fields {
			if strings.TrimSpace(field.Name) == "" || strings.TrimSpace(field.Type) == "" || len(field.Name) > 256 || len(field.Type) > 64 {
				return input, errors.New("panelContext contains an invalid field")
			}
		}
		if len(input.PanelContext.DataHint) > maxDataHintBytes || !utf8.ValidString(input.PanelContext.DataHint) {
			return input, fmt.Errorf("panelContext dataHint exceeds the %d-byte limit or is not valid UTF-8", maxDataHintBytes)
		}
		backend := input.PanelContext.RenderBackend
		if backend != "" && backend != "echarts" && backend != "vegalite" && backend != "plotly" && backend != "chartjs" {
			return input, errors.New("panelContext renderBackend is invalid")
		}
	}
	if input.PanelRef != nil {
		if err := validatePanelRef(input.PanelRef); err != nil {
			return input, err
		}
	}
	return input, nil
}

func validatePanelRef(ref *chatPanelRef) error {
	if ref.PanelID != nil {
		if *ref.PanelID < 0 || *ref.PanelID > maxPanelID {
			return errors.New("panelRef panelId is out of range")
		}
	}
	ref.PluginID = strings.TrimSpace(ref.PluginID)
	ref.Title = strings.TrimSpace(ref.Title)
	ref.TimeFrom = strings.TrimSpace(ref.TimeFrom)
	ref.TimeTo = strings.TrimSpace(ref.TimeTo)
	ref.TimeZone = strings.TrimSpace(ref.TimeZone)
	ref.DashboardUID = strings.TrimSpace(ref.DashboardUID)

	for _, value := range []string{ref.PluginID, ref.Title, ref.TimeFrom, ref.TimeTo, ref.TimeZone, ref.DashboardUID} {
		if len(value) > maxPanelRefStringBytes || !utf8.ValidString(value) {
			return fmt.Errorf("panelRef contains a string outside the 0 to %d byte UTF-8 limit", maxPanelRefStringBytes)
		}
	}
	return nil
}

func buildChatContextMessage(input chatRequest) (string, error) {
	parts := make([]string, 0, 2)
	if input.PanelContext != nil {
		encoded, err := json.Marshal(input.PanelContext)
		if err != nil {
			return "", errors.New("could not encode panel context")
		}
		parts = append(parts, "Flint Panel schema (read-only): "+string(encoded))
	}
	if input.PanelRef != nil {
		encoded, err := json.Marshal(input.PanelRef)
		if err != nil {
			return "", errors.New("could not encode panel reference")
		}
		parts = append(parts, "Grafana panel reference (read-only): "+string(encoded))
	}
	if len(parts) == 0 {
		return "No panel context was supplied. Answer generally about Grafana panels without claiming you inspected live query results.", nil
	}
	return strings.Join(parts, "\n"), nil
}

func validateConversation(messages []chatMessage, maxMessages int, maxBytes int, allowEmpty bool) error {
	if (!allowEmpty && len(messages) == 0) || len(messages) > maxMessages {
		return fmt.Errorf("must contain between 1 and %d entries", maxMessages)
	}
	total := 0
	for index := range messages {
		messages[index].Role = strings.TrimSpace(messages[index].Role)
		messages[index].Content = strings.TrimSpace(messages[index].Content)
		message := messages[index]
		if message.Role != "user" && message.Role != "assistant" {
			return errors.New("contains an invalid role")
		}
		if message.Content == "" || len(message.Content) > maxPromptBytes || !utf8.ValidString(message.Content) {
			return fmt.Errorf("contains content outside the 1 to %d byte limit", maxPromptBytes)
		}
		total += len(message.Content)
	}
	if total > maxBytes {
		return fmt.Errorf("exceeds the %d-byte limit", maxBytes)
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func decodeProviderProposal(content string, input generateChartRequest) (generateChartResponse, error) {
	if content == "" || len(content) > maxProposalBytes {
		return generateChartResponse{}, errors.New("AI provider returned an empty or oversized proposal")
	}
	var proposal generateChartResponse
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil || ensureJSONEOF(decoder) != nil {
		return generateChartResponse{}, errors.New("AI provider proposal was not a valid Flint JSON object")
	}
	if err := validateProposal(&proposal, input); err != nil {
		return generateChartResponse{}, err
	}
	return proposal, nil
}

func decodeProviderModelIDs(body []byte) ([]string, error) {
	var envelope struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data == nil {
		return nil, errors.New("AI provider returned a malformed models response")
	}
	if len(envelope.Data) > maxModelCatalogEntries {
		return nil, errors.New("AI provider returned too many models")
	}

	seen := make(map[string]struct{}, len(envelope.Data))
	models := make([]string, 0, len(envelope.Data))
	for _, entry := range envelope.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" || len(id) > maxModelIDBytes || !utf8.ValidString(id) {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}

func validateProposal(proposal *generateChartResponse, input generateChartRequest) error {
	proposal.ChartType = normalizeProposalChartType(proposal.ChartType, input.ChartCatalog)
	if proposal.ChartType == "" || len(proposal.ChartType) > 128 {
		return errors.New("AI provider proposal has an invalid chartType")
	}
	knownFields := make(map[string]struct{}, len(input.Fields))
	for _, field := range input.Fields {
		knownFields[field.Name] = struct{}{}
	}
	for _, value := range []string{proposal.XField, proposal.YField, proposal.ColorField} {
		if len(value) > 256 {
			return errors.New("AI provider proposal has an oversized field name")
		}
		if value != "" {
			if _, exists := knownFields[value]; !exists {
				return fmt.Errorf("AI provider proposal references unknown field %q", value)
			}
		}
	}
	if len(proposal.SpecJSON) > maxProposalBytes || len(proposal.Rationale) > 4*1024 {
		return errors.New("AI provider proposal contains an oversized value")
	}
	if proposal.SpecJSON != "" {
		var legacySpec any
		if err := json.Unmarshal([]byte(proposal.SpecJSON), &legacySpec); err != nil {
			return errors.New("AI provider legacy specJson is not valid JSON")
		}
		if hasForbiddenChartInputKey(legacySpec) {
			return errors.New("AI provider legacy specJson contains data, datasource, query, or credential material")
		}
	}
	if proposal.ChartInput != nil {
		encoded, err := json.Marshal(proposal.ChartInput)
		if err != nil || len(encoded) > maxProposalBytes {
			return errors.New("AI provider proposal contains an oversized chartInput")
		}
		if proposal.SpecJSON != "" {
			return errors.New("AI provider proposal must use chartInput or legacy specJson, not both")
		}
		if hasForbiddenChartInputKey(proposal.ChartInput) {
			return errors.New("AI provider chartInput contains data, datasource, query, or credential material")
		}
		if err := validateStructuredChartInput(proposal.ChartInput, proposal.ChartType, input); err != nil {
			return err
		}
	}
	return nil
}

func hasForbiddenChartInputKey(value any) bool {
	return hasForbiddenChartInputKeyAt(value, false)
}

func hasForbiddenChartInputKeyAt(value any, fieldNameMap bool) bool {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			if hasForbiddenChartInputKeyAt(child, fieldNameMap) {
				return true
			}
		}
	case map[string]any:
		for key, child := range typed {
			if !fieldNameMap {
				normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
				switch normalized {
				case "data", "datasource", "query", "apikey", "authorization", "credential", "password", "secret", "token":
					return true
				}
			}
			nextIsFieldNameMap := key == "semantic_types" || key == "field_display_names"
			if hasForbiddenChartInputKeyAt(child, nextIsFieldNameMap) {
				return true
			}
		}
	}
	return false
}

func validateStructuredChartInput(chartInput map[string]any, outerChartType string, input generateChartRequest) error {
	for key := range chartInput {
		if key != "semantic_types" && key != "field_display_names" && key != "chart_spec" {
			return fmt.Errorf("AI provider chartInput has unsupported root property %q", key)
		}
	}
	chartSpec, ok := chartInput["chart_spec"].(map[string]any)
	if !ok {
		return errors.New("AI provider chartInput requires chart_spec")
	}
	chartType, ok := chartSpec["chartType"].(string)
	if !ok || chartType != outerChartType {
		return errors.New("AI provider chartInput chartType must match the proposal chartType")
	}
	var catalog *chartCatalogEntry
	for index := range input.ChartCatalog {
		if input.ChartCatalog[index].ChartType == chartType {
			catalog = &input.ChartCatalog[index]
			break
		}
	}
	if catalog == nil {
		return errors.New("AI provider chartInput uses an unsupported chartType")
	}
	encodings, ok := chartSpec["encodings"].(map[string]any)
	if !ok {
		return errors.New("AI provider chartInput requires encodings")
	}
	allowedChannels := make(map[string]struct{}, len(catalog.Channels))
	for _, channel := range catalog.Channels {
		allowedChannels[channel] = struct{}{}
	}
	knownFields := make(map[string]struct{}, len(input.Fields))
	for _, field := range input.Fields {
		knownFields[field.Name] = struct{}{}
	}
	for channel, encoding := range encodings {
		if _, ok := allowedChannels[channel]; !ok {
			return fmt.Errorf("AI provider chartInput uses unsupported channel %q", channel)
		}
		if encoding == nil {
			continue
		}
		if err := validateEncodingFields(encoding, knownFields); err != nil {
			return fmt.Errorf("AI provider chartInput channel %s: %w", channel, err)
		}
	}
	for _, channel := range catalog.RequiredChannels {
		if value, ok := encodings[channel]; !ok || value == nil {
			return fmt.Errorf("AI provider chartInput is missing required channel %q", channel)
		}
	}
	semantics, present := chartInput["semantic_types"]
	semanticMap, ok := semantics.(map[string]any)
	if !present || !ok {
		return errors.New("AI provider chartInput semantic_types must annotate every encoded field")
	}
	allowedTypes := make(map[string]struct{}, len(input.SemanticTypes))
	for _, semanticType := range input.SemanticTypes {
		allowedTypes[semanticType] = struct{}{}
	}
	for field, value := range semanticMap {
		if value == nil {
			continue
		}
		if _, ok := knownFields[field]; !ok {
			return fmt.Errorf("AI provider chartInput semantic_types references unknown field %q", field)
		}
		semanticType, ok := value.(string)
		if !ok {
			return fmt.Errorf("AI provider chartInput semantic_types.%s must be a string", field)
		}
		if _, ok := allowedTypes[semanticType]; !ok {
			return fmt.Errorf("AI provider chartInput uses unknown semantic type %q", semanticType)
		}
	}
	encodedFields := make(map[string]struct{})
	for _, encoding := range encodings {
		collectEncodingFields(encoding, encodedFields)
	}
	for field := range encodedFields {
		if value, ok := semanticMap[field].(string); !ok || value == "" {
			return fmt.Errorf("AI provider chartInput semantic_types must annotate encoded field %q", field)
		}
	}
	return nil
}

func collectEncodingFields(value any, fields map[string]struct{}) {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			collectEncodingFields(child, fields)
		}
	case map[string]any:
		if field, ok := typed["field"].(string); ok {
			fields[field] = struct{}{}
		}
	case string:
		fields[typed] = struct{}{}
	}
}

func validateEncodingFields(value any, knownFields map[string]struct{}) error {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return errors.New("encoding array is empty")
		}
		for _, child := range typed {
			if err := validateEncodingFields(child, knownFields); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		field, ok := typed["field"].(string)
		if !ok {
			return errors.New("encoding requires a field")
		}
		if _, ok := knownFields[field]; !ok {
			return fmt.Errorf("encoding references unknown field %q", field)
		}
		return nil
	case string:
		if _, ok := knownFields[typed]; !ok {
			return fmt.Errorf("encoding references unknown field %q", typed)
		}
		return nil
	default:
		return errors.New("encoding must be an object or array")
	}
}

func normalizeProposalChartType(value string, catalog []chartCatalogEntry) string {
	trimmed := strings.TrimSpace(value)
	allowed := make(map[string]struct{}, len(catalog))
	for _, entry := range catalog {
		allowed[entry.ChartType] = struct{}{}
	}
	if _, exists := allowed[trimmed]; exists {
		return trimmed
	}
	aliases := map[string]string{
		"line": "Line Chart", "line chart": "Line Chart",
		"area": "Area Chart", "area chart": "Area Chart",
		"bar": "Bar Chart", "bar chart": "Bar Chart",
		"grouped": "Grouped Bar Chart", "grouped bar": "Grouped Bar Chart",
		"stacked": "Stacked Bar Chart", "stacked bar": "Stacked Bar Chart",
		"scatter": "Scatter Plot", "scatter plot": "Scatter Plot",
		"pie": "Pie Chart", "pie chart": "Pie Chart", "donut": "Pie Chart", "donut chart": "Pie Chart",
		"heatmap": "Heatmap", "histogram": "Histogram", "gauge": "Gauge Chart", "radar": "Radar Chart",
	}
	canonical := aliases[strings.ToLower(trimmed)]
	if _, exists := allowed[canonical]; exists {
		return canonical
	}
	return ""
}

func providerErrorMessage(body []byte, apiKey string) string {
	var value struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &value) != nil {
		return ""
	}
	message := strings.TrimSpace(value.Error.Message)
	if message == "" {
		message = strings.TrimSpace(value.Message)
	}
	if message == "" {
		message = strings.TrimSpace(value.Detail)
	}
	if apiKey != "" {
		message = strings.ReplaceAll(message, apiKey, "<redacted>")
	}
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
