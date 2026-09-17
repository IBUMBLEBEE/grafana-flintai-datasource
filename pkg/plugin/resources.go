package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

	const (
		maxRequestBytes  = 32 * 1024
		maxPromptBytes   = 4 * 1024
		maxDataHintBytes = 4 * 1024
		maxChatMessages  = 10
		maxChatBytes     = 16 * 1024
		// Some OpenAI-compatible providers include large reasoning or metadata envelopes.
		// The extracted Flint proposal remains capped separately at maxProposalBytes.
		maxProviderResponseBytes    = 1024 * 1024
		maxProposalBytes            = 64 * 1024
		maxChatResponseBytes        = 64 * 1024
		maxFields                   = 128
		maxProviderCompletionTokens = 1024
		maxPanelRefStringBytes      = 256
		maxPanelID                  = 1_000_000_000
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
		providerKind: normalizeProviderKind(strings.TrimSpace(raw.ProviderKind)),
		apiKey:       strings.TrimSpace(settings.DecryptedSecureJSONData["apiKey"]),
	}, nil
}

func (config providerConfig) validate() error {
	parsed, err := url.ParseRequestURI(config.baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("Flint AI Base URL must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" {
		if parsed.Scheme != "http" || os.Getenv(allowHTTPEnv) != "true" || !isLocalDevelopmentHost(parsed.Hostname()) {
			return errors.New("Flint AI Base URL must use HTTPS (local HTTP requires explicit backend development opt-in)")
		}
	}
	if _, err := adapterForProvider(config.providerKind); err != nil {
		return err
	}
	if config.model == "" || len(config.model) > 256 {
		return errors.New("Flint AI model is not configured or is too long")
	}
	if config.apiKey == "" {
		return errors.New("Flint AI API key is not configured")
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
	ChartType string   `json:"chartType"`
	Channels  []string `json:"channels"`
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
	FrameSummary       []frameSummary      `json:"frameSummary,omitempty"`
	Conversation       []chatMessage       `json:"conversation,omitempty"`
}

type generateChartResponse struct {
	ChartType  string `json:"chartType"`
	XField     string `json:"xField,omitempty"`
	YField     string `json:"yField,omitempty"`
	ColorField string `json:"colorField,omitempty"`
	SpecJSON   string `json:"specJson,omitempty"`
	Rationale  string `json:"rationale,omitempty"`
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

const chatSystemPrompt = `You are a read-only conversational assistant for Grafana Dashboard panels.
Use only the bounded context supplied with the request. Help the user reason about the panel, its visualization, time range, and related observability questions.
Never claim that you changed the Panel, datasource, query, dashboard, alert, or Grafana configuration. You cannot execute, modify, or save anything.
When Flint Panel schema fields are present, you may discuss chart choice and encodings; Generate/Apply remain separate actions outside this chat.
Do not request secrets or credentials. Keep the answer concise and directly useful.`

func (ds *Datasource) handleHealth(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": ds.config.validate() == nil})
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
	proposal, err := ds.generate(request, input)
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
	message, err := ds.chat(request, input)
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
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" || len(input.Prompt) > maxPromptBytes || !utf8.ValidString(input.Prompt) {
		return input, fmt.Errorf("prompt must be valid UTF-8 between 1 and %d bytes", maxPromptBytes)
	}
	if len(input.Fields) == 0 || len(input.Fields) > maxFields {
		return input, fmt.Errorf("fields must contain between 1 and %d entries", maxFields)
	}
	seen := make(map[string]struct{}, len(input.Fields))
	for i := range input.Fields {
		input.Fields[i].Name = strings.TrimSpace(input.Fields[i].Name)
		input.Fields[i].Type = strings.TrimSpace(input.Fields[i].Type)
		field := input.Fields[i]
		if field.Name == "" || field.Type == "" || len(field.Name) > 256 || len(field.Type) > 64 {
			return input, errors.New("fields contain an invalid name or type")
		}
		if _, exists := seen[field.Name]; exists {
			return input, errors.New("fields contain a duplicate name")
		}
		seen[field.Name] = struct{}{}
	}
	if len(input.DataHint) > maxDataHintBytes || !utf8.ValidString(input.DataHint) {
		return input, fmt.Errorf("dataHint exceeds the %d-byte limit or is not valid UTF-8", maxDataHintBytes)
	}
	if len(input.SuggestedChartType) > 128 {
		return input, errors.New("suggestedChartType is too long")
	}
	if input.RenderBackend != "echarts" && input.RenderBackend != "vegalite" && input.RenderBackend != "plotly" && input.RenderBackend != "chartjs" {
		return input, errors.New("renderBackend is invalid")
	}
	if len(input.ChartCatalog) == 0 || len(input.ChartCatalog) > 64 {
		return input, errors.New("chartCatalog must contain between 1 and 64 entries")
	}
	seenCharts := make(map[string]struct{}, len(input.ChartCatalog))
	for _, entry := range input.ChartCatalog {
		if strings.TrimSpace(entry.ChartType) == "" || len(entry.ChartType) > 128 || len(entry.Channels) > 24 {
			return input, errors.New("chartCatalog contains an invalid entry")
		}
		if _, exists := seenCharts[entry.ChartType]; exists {
			return input, errors.New("chartCatalog contains a duplicate chartType")
		}
		seenCharts[entry.ChartType] = struct{}{}
		for _, channel := range entry.Channels {
			if strings.TrimSpace(channel) == "" || len(channel) > 64 {
				return input, errors.New("chartCatalog contains an invalid channel")
			}
		}
	}
	if len(input.FrameSummary) > 64 {
		return input, errors.New("frameSummary contains too many entries")
	}
	for _, frame := range input.FrameSummary {
		if frame.FrameIndex < 0 || len(frame.RefID) > 128 || len(frame.Fields) > maxFields {
			return input, errors.New("frameSummary contains an invalid frame")
		}
		for _, field := range frame.Fields {
			if strings.TrimSpace(field) == "" || len(field) > 256 {
				return input, errors.New("frameSummary contains an invalid field")
			}
		}
	}
	if err := validateConversation(input.Conversation, maxChatMessages, maxChatBytes, true); err != nil {
		return input, fmt.Errorf("conversation %w", err)
	}
	return input, nil
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

func (ds *Datasource) generate(request *http.Request, input generateChartRequest) (generateChartResponse, error) {
	adapter, err := adapterForProvider(ds.config.providerKind)
	if err != nil {
		return generateChartResponse{}, err
	}
	providerRequest, err := adapter.buildGenerateRequest(ds.config, input)
	if err != nil {
		return generateChartResponse{}, err
	}
	content, err := ds.sendProviderRequest(request, providerRequest)
	if err != nil {
		return generateChartResponse{}, err
	}
	proposal, proposalErr := decodeProviderProposal(content, input)
	if proposalErr == nil {
		return proposal, nil
	}

	// Both supported providers can still return syntactically valid but invalid
	// proposal values. Give the same adapter one bounded correction attempt.
	if content == "" || len(content) > maxProposalBytes {
		return generateChartResponse{}, proposalErr
	}
	providerRequest.Messages = append(providerRequest.Messages,
		chatMessage{Role: "assistant", Content: content},
		chatMessage{
			Role:    "user",
			Content: "Correct the previous proposal. Validation error: " + proposalErr.Error() + ". Return only the corrected structured proposal.",
		},
	)
	correctedContent, err := ds.sendProviderRequest(request, providerRequest)
	if err != nil {
		return generateChartResponse{}, err
	}
	return decodeProviderProposal(correctedContent, input)
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

func (ds *Datasource) chat(request *http.Request, input chatRequest) (string, error) {
	adapter, err := adapterForProvider(ds.config.providerKind)
	if err != nil {
		return "", err
	}
	contextMessage, err := buildChatContextMessage(input)
	if err != nil {
		return "", err
	}
	messages := make([]chatMessage, 0, len(input.Messages)+2)
	messages = append(messages, chatMessage{Role: "system", Content: chatSystemPrompt})
	messages = append(messages, chatMessage{Role: "system", Content: contextMessage})
	messages = append(messages, input.Messages...)
	content, err := ds.sendProviderRequest(request, adapter.buildChatRequest(ds.config, messages))
	if err != nil {
		return "", err
	}
	if content == "" || len(content) > maxChatResponseBytes || !utf8.ValidString(content) {
		return "", errors.New("AI provider returned an empty or oversized chat response")
	}
	return content, nil
}

func (ds *Datasource) sendProviderRequest(request *http.Request, providerRequest upstreamRequest) (string, error) {
	payload, err := marshalProviderRequest(providerRequest)
	if err != nil {
		return "", errors.New("could not encode provider request")
	}
	return ds.requestProviderContent(request, payload)
}

func (ds *Datasource) requestProviderContent(request *http.Request, payload []byte) (string, error) {
	upstream, err := http.NewRequestWithContext(request.Context(), http.MethodPost, ds.config.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", errors.New("could not create provider request")
	}
	upstream.Header.Set("Authorization", "Bearer "+ds.config.apiKey)
	upstream.Header.Set("Content-Type", "application/json")
	response, err := ds.client.Do(upstream)
	if err != nil {
		return "", errors.New("AI provider request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil || len(body) > maxProviderResponseBytes {
		return "", errors.New("AI provider response exceeded the size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail := providerErrorMessage(body, ds.config.apiKey)
		if detail != "" {
			return "", fmt.Errorf("AI provider returned HTTP %d: %s", response.StatusCode, detail)
		}
		return "", fmt.Errorf("AI provider returned HTTP %d", response.StatusCode)
	}

	var providerReply upstreamResponse
	if err := json.Unmarshal(body, &providerReply); err != nil || len(providerReply.Choices) == 0 {
		return "", errors.New("AI provider returned a malformed response")
	}
	return strings.TrimSpace(providerReply.Choices[0].Message.Content), nil
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
	return nil
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
