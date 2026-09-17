# Chat DTO v1

Shared contract for `POST` datasource resource `/chat`.  
Authoritative implementation: Go `pkg/plugin/resources.go` + TypeScript `src/types.ts`.

## Request

```json
{
  "messages": [{ "role": "user", "content": "…" }],
  "panelContext": {
    "fields": [{ "name": "region", "type": "string" }],
    "dataHint": "12 rows",
    "renderBackend": "echarts"
  },
  "panelRef": {
    "panelId": 12,
    "pluginId": "timeseries",
    "title": "CPU",
    "timeFrom": "now-1h",
    "timeTo": "now",
    "timeZone": "browser",
    "dashboardUid": "ops"
  }
}
```

| Field | Required | Notes |
| --- | --- | --- |
| `messages` | yes | `user`/`assistant` only; 1–10 messages; ≤16 KiB total; ≤4 KiB each |
| `panelContext` | no | Flint Panel schema for existing Flint Panel callers. |
| `panelRef` | no | Optional bounded panel identity retained for backward compatibility. Strings ≤256 bytes UTF-8; `panelId` in `[0, 1e9]`. |

Unknown JSON fields are rejected (`DisallowUnknownFields`).

## Response

```json
{ "message": "…" }
```

## Callers

- Existing Flint Panel callers: `messages` + optional `panelContext` / `panelRef`
