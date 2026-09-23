# Provider / model selection progress

SSOT: [`provider-model-selection-execution-plan.md`](./provider-model-selection-execution-plan.md)

## T1 - 已验证 - 2026-09-21

- 完成：前后端只接受 `openai` / `deepseek`；OpenAI、DeepSeek Token 分别保存在 `openaiApiKey` / `deepseekApiKey`；不存在通用 secret fallback；Provider 切换清空 Model、保留两方 Token，并按默认/自定义 Base URL 规则处理。
- 改动：`src/types.ts`、`src/configHelpers.ts`、`src/ConfigEditor.tsx`、`pkg/plugin/resources.go`、`pkg/plugin/provider_adapter.go` 及对应测试。
- 验证：严格 Provider、Provider 专属 Token、新 datasource 默认配置、切换与 reset 测试通过。
- 未决：无。

## T2 - 已验证 - 2026-09-21

- 完成：建立 `LLMProvider` seam；Datasource resource handler 只调用 `Chat`、`Generate`、`ListModels`；Provider adapter 隐藏请求参数和模型 endpoint 差异；共享 transport 负责认证、超时、取消、响应上限、解析和脱敏。
- 改动：新增 `pkg/plugin/provider_client.go`；更新 `pkg/plugin/provider_adapter.go`、`pkg/plugin/datasource.go`、`pkg/plugin/resources.go` 及测试。
- 验证：Go adapter、resource 和 provider 集成测试通过；OpenAI 使用 `max_completion_tokens` + `json_schema`，DeepSeek 使用 `max_tokens` + `json_object`。
- 未决：无。

## T3 - 已验证 - 2026-09-21

- 完成：`GET /models` 使用已保存 Provider 配置；OpenAI 请求 `<base>/models`，DeepSeek 去除末尾 `/v1` 后请求同 host `/models`；目录只返回过滤、去重、排序后的 ID；无 Model 时允许加载目录。
- 改动：`pkg/plugin/provider_client.go`、`pkg/plugin/provider_adapter.go`、`pkg/plugin/resources.go` 及测试。
- 验证：标准/空/畸形/超大响应、401/403/429/5xx、网络失败、secret redaction、URL 安全和“不调用 chat endpoint”测试通过。
- 未决：无。

## T4 - 已验证 - 2026-09-21

- 完成：Config Editor 支持 Provider、Base URL、独立 Token、模型目录、目录选择和任意自定义 Model ID；未保存连接配置禁用目录加载；未知 Provider 不会静默当作 OpenAI。
- 改动：`src/ConfigEditor.tsx`、`src/configHelpers.ts`、`src/types.ts` 及测试。
- 验证：Jest 覆盖目录加载、选择、自定义、失败保留、Provider 切换、新实例初始化和 secret 状态，共 19 项测试通过。
- 未决：无。

## T5 - 已验证 - 2026-09-21

- 完成：Provisioning 使用 Provider 专属 secret 字段；本地 stub 同时覆盖 OpenAI `/v1/models`、DeepSeek `/models` 和 `/chat/completions`；README、CHANGELOG 与当前配置契约一致。
- 改动：`provisioning/datasources/flint-ai.yml`、`tests/configEditor.spec.ts`、`scripts/stub-provider/`、`README.md`、`CHANGELOG.md`。
- E2E：Grafana 13.1.0 + 本地 stub，OpenAI/DeepSeek 均完成目录加载、目录选择、自定义 Model 保存和 `/chat` 调用；未保存凭据、认证失败和模型目录不可用时手工输入的路径也通过，共 6 项测试通过。
- 未决：无。

## T6 - 已验证 - 2026-09-21

- 完成：新增 `POST /models/preview`，允许配置页用未保存的 Provider、Base URL 和 API Key 临时加载模型，连接信息不持久化；Provider client 拒绝跨 host/scheme 重定向。
- 完成：Model 改为单一可搜索 Combobox，打开即加载目录，并继续允许自定义 Model ID；移除 Load models 按钮和 From catalog 字段。
- 改动：`pkg/plugin/datasource.go`、`pkg/plugin/resources.go`、`src/ConfigEditor.tsx`、前后端测试及文档。
- 验证：Go resource 测试、21 项 Jest 测试、TypeScript 类型检查、webpack/Mage 构建通过；本地 Grafana + stub Provider 的 6 项配置页 E2E 全部通过。
- 未决：无。

## T7 - 已验证 - 2026-09-21

- 修复：DeepSeek / OpenAI 切换后再切回时，已保存或刚选择的模型不再丢失。
- 实现：`jsonData.model` 继续作为当前 Provider 的后端执行值；`openaiModel` / `deepseekModel` 分别记忆两方选择，兼容既有单 `model` 配置。
- 验证：helper 与带状态回写的 Config Editor 回归测试覆盖 DeepSeek → OpenAI → DeepSeek 恢复路径。
- 未决：无。

## T8 - 已验证 - 2026-09-21

- 完成：Model Combobox 明确支持输入目录外的自定义 Model ID 并按 Enter 确认；目录不可用、网络失败或返回空目录时均给出手工输入指引。
- 校验：前端与后端一致要求 Model ID 去除首尾空白后非空，且不超过 256 UTF-8 bytes。
- 回归：配置页 E2E 覆盖模型目录不可用的 AI 网关场景，包括手工输入、保存、刷新后仍保留自定义 Model ID。
- 未决：无。

## T9 - 已验证 - 2026-09-21

- 完成：Config Editor 新增 **Test AI connection**，通过 `POST /test` 使用当前 Provider、Base URL、API Key 和 Model 发起最小 chat-completion 请求，并显示成功或脱敏后的失败信息。
- 安全：最大输出限制为 16 token，不把模型回复返回浏览器；已保存 Token 只在 Provider 与 Base URL 精确匹配时复用，临时 Token 不持久化。
- 验证：Go 覆盖 saved/transient 配置、OpenAI/DeepSeek 请求形状、secret redaction 和跨 Base URL 拒绝；Jest 覆盖 UI 成功、失败与未保存配置；E2E 覆盖两种 Provider 的实际配置页测试流程。
- 未决：无。

## T10 - 已验证 - 2026-09-23

- 完成：`POST /models/preview` 与 `POST /test` 现在要求 Grafana 组织管理员；普通 Viewer/Editor 在 backend resource 层收到 403。
- 安全：Provider transport 对 DNS 全部结果执行公网地址校验，拒绝私有与特殊用途网络，直接拨号到已校验 IP，并禁用可绕过该策略的进程级 HTTP proxy。
- 防滥用：所有 Provider-backed resource 共享每位用户每分钟 30 次的限制与每个 datasource instance 4 个并发槽位。
- 供应链：GitHub Actions 全部固定到完整 commit SHA；CI 新增生产依赖审计、`govulncheck` 与 `gosec`。
- 验证：新增 DNS rebinding、特殊地址、管理员权限、速率窗口和并发上限回归测试；`gosec` 无 finding，Go race tests 与 Grafana 13.1 E2E 6/6 通过。
- 未决：`@grafana/ui` 间接依赖的四项 moderate React Router advisory 仍等待上游兼容修复；相关模块未打包进插件产物。

## 完整门禁

以下命令在 2026-09-21 通过：

```text
npm run typecheck
npm run lint
npm run test:ci
npm run build
go test ./... -count=1
npm run build:backend
NO_PROXY=127.0.0.1,localhost GRAFANA_URL=http://127.0.0.1:3300 npm run e2e
```

E2E 使用 `3300` 是因为本机 `3000` 已被另一个 Grafana 容器占用；未停止或修改该容器。未使用真实 Provider Token，也未发送计费请求。
