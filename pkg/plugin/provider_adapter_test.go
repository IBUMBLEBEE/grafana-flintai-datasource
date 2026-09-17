package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAIAdapterUsesStrictDynamicProposalSchema(t *testing.T) {
	request, err := (openAIAdapter{}).buildGenerateRequest(validConfig("https://api.openai.com/v1"), validGenerateRequest())
	if err != nil {
		t.Fatalf("build OpenAI request: %v", err)
	}
	if request.MaxCompletionTokens != maxProviderCompletionTokens || request.MaxTokens != 0 {
		t.Fatalf("unexpected OpenAI token controls: %#v", request)
	}
	if request.Thinking != nil || request.Temperature != nil {
		t.Fatalf("OpenAI request contains provider-specific controls: %#v", request)
	}

	format, ok := request.ResponseFormat.(map[string]any)
	if !ok || format["type"] != "json_schema" {
		t.Fatalf("OpenAI request must use JSON Schema: %#v", request.ResponseFormat)
	}
	jsonSchema, ok := format["json_schema"].(map[string]any)
	if !ok || jsonSchema["strict"] != true {
		t.Fatalf("OpenAI schema must be strict: %#v", format)
	}
	schema, ok := jsonSchema["schema"].(map[string]any)
	if !ok || schema["additionalProperties"] != false {
		t.Fatalf("OpenAI schema must reject additional properties: %#v", jsonSchema)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	for _, expected := range []string{`"Bar Chart"`, `"Pie Chart"`, `"region"`, `"revenue"`} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("schema does not contain runtime enum %s: %s", expected, encoded)
		}
	}
}

func TestDeepSeekAdapterUsesJSONModeAndDeepSeekControls(t *testing.T) {
	config := validConfig("https://api.deepseek.com/v1")
	config.providerKind = providerKindDeepSeek
	request, err := (deepSeekAdapter{}).buildGenerateRequest(config, validGenerateRequest())
	if err != nil {
		t.Fatalf("build DeepSeek request: %v", err)
	}
	if request.MaxTokens != maxProviderCompletionTokens || request.MaxCompletionTokens != 0 {
		t.Fatalf("unexpected DeepSeek token controls: %#v", request)
	}
	if request.Thinking["type"] != "disabled" || request.Temperature == nil || *request.Temperature != 0.2 {
		t.Fatalf("unexpected DeepSeek controls: %#v", request)
	}
	format, ok := request.ResponseFormat.(map[string]string)
	if !ok || format["type"] != "json_object" {
		t.Fatalf("DeepSeek request must use JSON mode: %#v", request.ResponseFormat)
	}
	if !strings.Contains(request.Messages[0].Content, "EXAMPLE JSON OUTPUT") || !strings.Contains(strings.ToLower(request.Messages[0].Content), "json") {
		t.Fatalf("DeepSeek JSON mode must be grounded by its prompt: %q", request.Messages[0].Content)
	}
}

func TestGenerateMessagesPreserveConversationRolesAndSeparatePanelContext(t *testing.T) {
	input := validGenerateRequest()
	input.Conversation = []chatMessage{
		{Role: "user", Content: "Compare regions."},
		{Role: "assistant", Content: "A bar chart would work."},
	}
	messages, err := buildGenerateMessages(input, flintProposalSystemPrompt)
	if err != nil {
		t.Fatalf("build generate messages: %v", err)
	}
	if len(messages) != 4 || messages[1].Role != "user" || messages[2].Role != "assistant" || messages[3].Role != "user" {
		t.Fatalf("conversation roles were not preserved: %#v", messages)
	}
	if strings.Contains(messages[3].Content, `"conversation"`) || !strings.Contains(messages[3].Content, `"frameSummary"`) {
		t.Fatalf("Panel context must be separate from conversation: %q", messages[3].Content)
	}
}

func TestProviderKindSupportsOnlyOpenAIAndDeepSeek(t *testing.T) {
	if normalizeProviderKind(legacyProviderKind) != providerKindOpenAI {
		t.Fatal("legacy provider kind should migrate to OpenAI")
	}
	if _, err := adapterForProvider("gemini"); err == nil {
		t.Fatal("unsupported provider should be rejected")
	}
}
