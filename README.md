# Flint AI data source

Grafana data source plugin (`ibumblebee-flint-ai-datasource`) that stores OpenAI-compatible provider credentials and exposes backend resource handlers:

- `GET /health` — configuration readiness (no billable Provider call)
- `POST /chat` — bounded conversational assist
- `POST /generate` — Flint Panel chart proposal (existing Assist flow)

Credentials live only in Grafana `secureJsonData.apiKey`. The browser never reads the key.

## Configuration

The datasource supports OpenAI and DeepSeek provider settings. Provider kind, Base URL, and selected model ID are stored in `jsonData`; the API key is stored in Grafana `secureJsonData`.

The current implementation accepts a model ID as text. Dynamic model discovery is tracked in the [Provider and model selection execution plan](docs/provider-model-selection-execution-plan.md); its official endpoint research is in [provider model-list API research](docs/provider-model-list-api-research.md).

The backend continues to expose `/health`, `/chat`, and `/generate` for existing Flint Panel callers. The datasource does not provide an App-level Panel menu or Chat Modal.

The existing [`Chat DTO v1`](docs/chat-dto-v1.md) documents the `/chat` resource contract.

## Develop

```bash
npm install
npm run build
npm run build:backend
npm run server
```

Local Grafana mounts `./dist`. Companion panel plugin (if used) lives at `../grafana-flint-panel`.

Grafana ≥12.4 no longer forwards host env into plugin processes by default. For local HTTP Provider stubs set:

```yaml
GF_PLUGINS_FORWARD_HOST_ENV_VARS: ibumblebee-flint-ai-datasource
FLINT_AI_ALLOW_LOCAL_HTTP: "true"
```

## License

Apache-2.0 — see `LICENSE`.
