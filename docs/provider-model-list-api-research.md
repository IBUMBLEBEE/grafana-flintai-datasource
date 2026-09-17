# Provider model-list API research

Verified against first-party documentation on 2026-09-17.

| Provider | Endpoint | Authentication | Response |
| --- | --- | --- | --- |
| OpenAI | `GET https://api.openai.com/v1/models` | `Authorization: Bearer <API key>` | JSON list with `object: "list"` and `data[]`; each model has an `id` usable in API requests, plus metadata such as `created` and `owned_by`. |
| DeepSeek | `GET https://api.deepseek.com/models` | Bearer API key | JSON list with `object: "list"` and `data[]`; each model has `id`, `object: "model"`, and `owned_by`. |

The endpoint references document no pagination parameters or pagination continuation fields for either model list; treat the returned `data` as the response, without assuming cursors. Both endpoints describe models available through that API/key, not model capabilities or feature compatibility. Keep the selected model ID as a string and do not infer that every returned model supports the datasource's eventual request shape. [OpenAI List Models](https://platform.openai.com/docs/api-reference/models/list), [OpenAI Authentication](https://platform.openai.com/docs/api-reference/authentication), [DeepSeek List Models](https://api-docs.deepseek.com/api/list-models/), [DeepSeek API authentication](https://api-docs.deepseek.com/api/deepseek-api/)

## Base URL handling

DeepSeek documents `https://api.deepseek.com` as its OpenAI-format base URL; its model-list path is `/models`. OpenAI documents its first-party endpoint as `/v1/models`. The official docs do not define a universal contract for arbitrary OpenAI-compatible gateways. Therefore, for a configurable custom base URL, make model discovery an attempted capability (using the configured base URL plus a documented/selected API path), surface failures, and retain a manual model-ID option. Do not silently substitute the OpenAI host or claim that a gateway supports `/models` based only on chat-completions compatibility. This is an implementation recommendation inferred from the providers' endpoint-specific documentation. [DeepSeek OpenAI-format usage](https://api-docs.deepseek.com/quick_start/pricing-details-cny/), [DeepSeek List Models](https://api-docs.deepseek.com/api/list-models/), [OpenAI List Models](https://platform.openai.com/docs/api-reference/models/list)
