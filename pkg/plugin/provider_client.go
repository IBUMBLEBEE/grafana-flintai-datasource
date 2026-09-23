package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// LLMProvider is the seam used by datasource resources. Provider-specific
// request shapes, endpoints, transport and response decoding stay behind it.
type LLMProvider interface {
	Chat(ctx context.Context, input chatRequest) (string, error)
	Generate(ctx context.Context, input generateChartRequest) (generateChartResponse, error)
	Repair(ctx context.Context, input repairChartRequest) (generateChartResponse, error)
	ListModels(ctx context.Context) ([]string, error)
	TestConnection(ctx context.Context) error
}

func (provider *providerClient) Repair(ctx context.Context, input repairChartRequest) (generateChartResponse, error) {
	providerRequest, err := provider.adapter.buildRepairRequest(provider.config, input)
	if err != nil {
		return generateChartResponse{}, err
	}
	content, err := provider.sendRequest(ctx, providerRequest)
	if err != nil {
		return generateChartResponse{}, err
	}
	return decodeProviderProposal(content, input.Request)
}

type providerClient struct {
	config  providerConfig
	client  *http.Client
	adapter providerAdapter
}

func newLLMProvider(config providerConfig, client *http.Client) (LLMProvider, error) {
	adapter, err := adapterForProvider(config.providerKind)
	if err != nil {
		return nil, err
	}
	return &providerClient{config: config, client: client, adapter: adapter}, nil
}

func (provider *providerClient) Generate(ctx context.Context, input generateChartRequest) (generateChartResponse, error) {
	providerRequest, err := provider.adapter.buildGenerateRequest(provider.config, input)
	if err != nil {
		return generateChartResponse{}, err
	}
	content, err := provider.sendRequest(ctx, providerRequest)
	if err != nil {
		return generateChartResponse{}, err
	}
	proposal, proposalErr := decodeProviderProposal(content, input)
	if proposalErr == nil {
		return proposal, nil
	}

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
	correctedContent, err := provider.sendRequest(ctx, providerRequest)
	if err != nil {
		return generateChartResponse{}, err
	}
	return decodeProviderProposal(correctedContent, input)
}

func (provider *providerClient) Chat(ctx context.Context, input chatRequest) (string, error) {
	contextMessage, err := buildChatContextMessage(input)
	if err != nil {
		return "", err
	}
	messages := make([]chatMessage, 0, len(input.Messages)+2)
	messages = append(messages, chatMessage{Role: "system", Content: chatSystemPrompt})
	messages = append(messages, chatMessage{Role: "system", Content: contextMessage})
	messages = append(messages, input.Messages...)
	content, err := provider.sendRequest(ctx, provider.adapter.buildChatRequest(provider.config, messages))
	if err != nil {
		return "", err
	}
	if content == "" || len(content) > maxChatResponseBytes || !utf8.ValidString(content) {
		return "", errors.New("AI provider returned an empty or oversized chat response")
	}
	return content, nil
}

func (provider *providerClient) ListModels(ctx context.Context) ([]string, error) {
	endpoint, err := provider.adapter.modelsURL(provider.config)
	if err != nil {
		return nil, &providerCallError{status: http.StatusServiceUnavailable, message: err.Error()}
	}
	// #nosec G704 -- production clients pin validated public DNS answers in secureProviderDialContext.
	upstream, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, &providerCallError{status: http.StatusBadGateway, message: "could not create provider models request"}
	}
	upstream.Header.Set("Authorization", "Bearer "+provider.config.apiKey)

	// #nosec G704 -- the production transport rejects private and special-use destinations before dialing.
	response, err := provider.client.Do(upstream)
	if err != nil {
		return nil, &providerCallError{status: http.StatusBadGateway, message: "Could not reach the provider; try again or enter a model ID"}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil || len(body) > maxProviderResponseBytes {
		return nil, unavailableModelsError()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			detail := providerErrorMessage(body, provider.config.apiKey)
			message := fmt.Sprintf("AI provider returned HTTP %d", response.StatusCode)
			if detail != "" {
				message += ": " + detail
			}
			return nil, &providerCallError{status: response.StatusCode, message: message}
		case http.StatusTooManyRequests:
			return nil, &providerCallError{status: http.StatusTooManyRequests, message: "AI provider rate limited the models request; try again later"}
		default:
			return nil, unavailableModelsError()
		}
	}

	models, err := decodeProviderModelIDs(body)
	if err != nil {
		return nil, unavailableModelsError()
	}
	return models, nil
}

func (provider *providerClient) TestConnection(ctx context.Context) error {
	request := provider.adapter.buildChatRequest(provider.config, []chatMessage{{
		Role:    "user",
		Content: "Reply with OK.",
	}})
	if provider.config.providerKind == providerKindOpenAI {
		request.MaxCompletionTokens = maxProviderTestTokens
	} else {
		request.MaxTokens = maxProviderTestTokens
	}

	_, err := provider.sendRequest(ctx, request)
	return err
}

func unavailableModelsError() error {
	return &providerCallError{
		status:  http.StatusBadGateway,
		message: "Model list is unavailable for this Base URL; enter a model ID manually",
	}
}

type providerCallError struct {
	status  int
	message string
}

func (err *providerCallError) Error() string { return err.message }

func providerCallStatus(err error) int {
	var callErr *providerCallError
	if errors.As(err, &callErr) {
		return callErr.status
	}
	return http.StatusBadGateway
}

func (provider *providerClient) sendRequest(ctx context.Context, request upstreamRequest) (string, error) {
	payload, err := marshalProviderRequest(request)
	if err != nil {
		return "", errors.New("could not encode provider request")
	}
	return provider.requestContent(ctx, payload)
}

func (provider *providerClient) requestContent(ctx context.Context, payload []byte) (string, error) {
	// #nosec G704 -- provider.config is validated and production dials are constrained by secureProviderDialContext.
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.config.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", errors.New("could not create provider request")
	}
	upstream.Header.Set("Authorization", "Bearer "+provider.config.apiKey)
	upstream.Header.Set("Content-Type", "application/json")
	// #nosec G704 -- the production transport rejects private and special-use destinations before dialing.
	response, err := provider.client.Do(upstream)
	if err != nil {
		return "", errors.New("AI provider request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil || len(body) > maxProviderResponseBytes {
		return "", errors.New("AI provider response exceeded the size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail := providerErrorMessage(body, provider.config.apiKey)
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
