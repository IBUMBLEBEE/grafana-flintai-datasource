# Changelog

## 0.1.1 - 2026-09-23

### Bug fixes

- Fix(release): preserve prior changelog entries
- Fix(ci): support releases from private repositories

## 0.1.0 - 2026-09-23

### Added

- Initial standalone release of the Flint AI data source plugin.
- Grafana 13.1.0 and later support.
- `/health`, `/chat`, and `/generate` resource endpoints with OpenAI / DeepSeek providers.
- `GET /models` provider model discovery through the datasource backend (no browser-side Provider calls).
- `POST /models/preview` transient model discovery for unsaved Base URL/API key edits, without persisting credentials.
- `POST /test` and Config Editor feedback for verifying current Provider, Base URL, API key, and Model with a minimal completion request.
- Config Editor model combobox that loads on open, searches catalog IDs, clearly supports Enter-to-use custom IDs, and validates the backend's 256-byte model-ID limit.
- Provider switching preserves independent OpenAI and DeepSeek model selections.
- Strict `openai` / `deepseek` configuration with independent encrypted Provider tokens and no legacy field migration.
- Chat DTO v1 optional `panelRef` for compatibility with existing Flint Panel callers.
- Two-stage release preparation with guarded annotated tags and a gated GitHub Actions draft-release workflow.

### Security

- Restricted transient provider previews and connection tests to Grafana organization administrators.
- Added DNS-pinned outbound connections that reject private and special-use destinations by default.
- Added per-user rate limits and a global concurrency cap for provider-backed resources.
- Pinned GitHub Actions to immutable commit SHAs.
