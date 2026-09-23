## Project knowledge

This repository contains a **Grafana datasource plugin** (`ibumblebee-flintai-datasource`).

Before any code change, read `@./.config/AGENTS/instructions.md`. Before E2E changes, also read `@./.config/AGENTS/e2e-testing.md`.

### Provider configuration and model discovery (current SSOT)

- Execution plan: `@docs/provider-model-selection-execution-plan.md`
- Progress / handoff: `@docs/provider-model-selection-progress.md`
- Provider model-list API research: `@docs/provider-model-list-api-research.md`
- Supported provider kinds: OpenAI and DeepSeek
- API keys remain in `secureJsonData`; model IDs and provider settings remain in `jsonData`
- Model catalog is fetched only via datasource resource `GET /models` using the saved backend key

### Historical notes

- Former App panel-menu / Chat Modal work belonged in `../flint-ai-app` and was removed; that App is a legacy empty shell.
- Do not revive App chat UI from this datasource unless product explicitly changes scope.
