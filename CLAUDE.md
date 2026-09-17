## Project knowledge

This repository contains a **Grafana datasource plugin** (`ibumblebee-flint-ai-datasource`).

Before any code change, read `@./.config/AGENTS/instructions.md`. Before E2E changes, also read `@./.config/AGENTS/e2e-testing.md`.

### Provider configuration and model discovery

- Execution plan (SSOT): `@docs/provider-model-selection-execution-plan.md`
- Provider model-list API research: `@docs/provider-model-list-api-research.md`
- Task progress / handoff: `@docs/provider-model-selection-progress.md`
- Supported provider kinds: OpenAI and DeepSeek
- API keys remain in `secureJsonData`; model IDs and provider settings remain in `jsonData`
