# Flint AI Datasource：OpenAI / DeepSeek Provider 实施方案

## 1. 目标与范围

本项目是新的 Grafana datasource plugin。它负责保存 LLM 连接配置，并为 Flint 的聊天和图表生成请求提供统一的后端入口。

首期只支持两个 Provider：

- OpenAI
- DeepSeek

管理员在 Grafana Datasource 配置页完成以下操作：

```text
选择 Provider
  -> 配置 API Base URL
  -> 配置当前 Provider 的 API Token
  -> 打开 Model 下拉框并加载 Provider 模型目录
  -> 从目录选择模型，或在同一输入框输入任意模型 ID
  -> 发起最小 AI 请求测试当前配置
  -> 保存 Datasource
```

范围内：

- OpenAI、DeepSeek Provider 选择。
- 两个 Provider 各自独立的 API Token。
- Provider 默认 Base URL 和用户自定义 Base URL。
- 后端代理的模型目录查询。
- 可搜索的模型选择和自定义 Model ID。
- `/health`、`/models`、`/models/preview`、`/test`、`/chat`、`/generate` datasource resource。
- Provider 请求参数差异、错误清洗、超时和响应大小限制。

范围外：

- 旧字段、旧 Provider 名称和旧 datasource 实例迁移。
- OpenAI、DeepSeek 之外的 Provider。
- 浏览器直接调用 Provider。
- 将模型目录硬编码或完整持久化到 datasource 配置。
- 根据模型名称推断模型能力。
- Grafana Dashboard、Panel 查询或 Panel JSON 的自动修改。

## 2. 核心决策

### 2.1 Datasource 实例级切换

`providerKind` 是 datasource 实例的已保存配置。Grafana 保存配置并重建后端实例后，新请求才使用新 Provider。

`/chat`、`/generate` 和 `GET /models` 不接受 Provider、Base URL 或 Token 覆盖参数。`POST /models/preview` 和 `POST /test` 只为配置页接受当前编辑值，不修改 datasource 实例配置，也不持久化 Token。

### 2.2 Provider 独立 Token

OpenAI 和 DeepSeek 分别使用独立的 `secureJsonData` 字段。配置页只展示当前 Provider 对应的 `SecretInput`，切换 Provider 不删除另一方已经保存的 Token。

### 2.3 Model 是自由字符串

模型目录只帮助用户选择，不构成允许列表。最终配置只保存一个 `model` 字符串：

- 用户可以从动态目录选择。
- 用户可以输入目录中不存在的自定义 Model ID，并按 Enter 确认。
- 模型目录不可用时仍可保存自定义 Model ID。
- Model ID 去除首尾空白后必须非空，且不超过 256 UTF-8 bytes，与后端校验保持一致。
- OpenAI 和 DeepSeek 分别记住自己的 Model；Provider 切换时恢复目标 Provider 的值，未配置时显示空值。

### 2.4 Provider seam

Datasource handler 只依赖一个小的 Provider interface。OpenAI 和 DeepSeek adapter 隐藏 URL、请求参数、结构化输出格式和响应解析差异。共享 HTTP transport 位于 adapter implementation 内部，不暴露给 resource handler。

## 3. 配置契约

### 3.1 前端类型

非敏感配置保存在 `jsonData`：

```ts
export const FLINT_AI_PROVIDER_KINDS = ['openai', 'deepseek'] as const;
export type FlintAiProviderKind = (typeof FLINT_AI_PROVIDER_KINDS)[number];

export interface FlintAiJsonData extends DataSourceJsonData {
  providerKind: FlintAiProviderKind;
  baseUrl: string;
  model: string;
  openaiModel?: string;
  deepseekModel?: string;
}
```

`model` 始终是当前 Provider 的后端执行值；两个可选字段保存各 Provider 的 UI 选择，兼容只有 `model` 的已有配置。

Token 保存在 Grafana 加密的 `secureJsonData`：

```ts
export interface FlintAiSecureJsonData {
  openaiApiKey?: string;
  deepseekApiKey?: string;
}
```

不得增加通用 `apiKey` 字段，也不得把 Token 写入 `jsonData`、URL、浏览器存储、日志或错误信息。

### 3.2 默认配置

| Provider | `providerKind` | 默认 Base URL                 | Model 输入                   |
| -------- | -------------- | ----------------------------- | ---------------------------- |
| OpenAI   | `openai`       | `https://api.openai.com/v1`   | 空值，等待目录选择或手动输入 |
| DeepSeek | `deepseek`     | `https://api.deepseek.com/v1` | 空值，等待目录选择或手动输入 |

配置中不提供静态默认模型。

### 3.3 Provider 切换规则

从 Provider A 切换到 Provider B 时：

1. 清空已加载的模型目录和模型加载错误。
2. 将当前 `jsonData.model` 记入当前 Provider 的模型字段。
3. 当前 Base URL 为空或等于 A 的默认值时，替换为 B 的默认值。
4. 当前 Base URL 是用户自定义值时保留，并提示用户确认该地址支持 B。
5. 切换到 B 对应的 Token 输入状态；A 的已保存 Token 保留。
6. 恢复目标 Provider 已记住的 Model；目标 Provider 未配置过时使用空值。
7. Provider、Base URL 或 Token 有未保存修改且当前 Token 已输入时，通过 preview resource 加载模型。

## 4. 后端模块设计

### 4.1 Provider interface

建议使用以下深模块 interface：

```go
type LLMProvider interface {
	Chat(ctx context.Context, input ChatInput) (string, error)
	Generate(ctx context.Context, input GenerateInput) (GenerateOutput, error)
	ListModels(ctx context.Context) ([]string, error)
	TestConnection(ctx context.Context) error
}
```

构造函数负责选择 adapter，并注入可测试的 HTTP client：

```go
func NewLLMProvider(config ProviderConfig, client *http.Client) (LLMProvider, error)
```

严格接受两个 Provider 值：

```go
type ProviderKind string

const (
	ProviderOpenAI   ProviderKind = "openai"
	ProviderDeepSeek ProviderKind = "deepseek"
)
```

空值和未知值直接返回配置错误，不推断默认 Provider。

### 4.2 后端配置

Grafana instance settings 解析成统一配置：

```go
type ProviderConfig struct {
	Kind    ProviderKind
	BaseURL string
	Model   string
	APIKey  string
}
```

`APIKey` 根据 `Kind` 从对应 secure 字段读取：

```text
openai   -> DecryptedSecureJSONData["openaiApiKey"]
deepseek -> DecryptedSecureJSONData["deepseekApiKey"]
```

### 4.3 Adapter implementation

```text
Datasource resources
        |
        v
   LLMProvider
    /       \
OpenAI     DeepSeek
Adapter     Adapter
    \       /
 Shared HTTP transport
```

共享 transport 负责：

- Bearer Authorization。
- 请求上下文取消。
- HTTP client 超时。
- 请求与响应大小限制。
- 非 2xx 状态映射。
- Provider 错误信息清洗。
- JSON 响应解码。
- Token 脱敏。

Adapter 负责：

- Chat 和 Generate 请求体。
- Provider 特有参数。
- 结构化输出格式。
- 模型目录 endpoint。
- Provider 响应到内部结果的转换。

## 5. Provider 请求契约

### 5.1 OpenAI

Chat / Generate：

```http
POST <baseUrl>/chat/completions
Authorization: Bearer <openaiApiKey>
Content-Type: application/json
```

请求策略：

- 使用 `max_completion_tokens`。
- Generate 使用 `response_format.type = json_schema`。
- Model 使用 `jsonData.model` 原值，不根据名称改写。

模型目录：

```http
GET <baseUrl>/models
Authorization: Bearer <openaiApiKey>
```

默认地址最终为 `https://api.openai.com/v1/models`。

### 5.2 DeepSeek

Chat / Generate：

```http
POST <baseUrl>/chat/completions
Authorization: Bearer <deepseekApiKey>
Content-Type: application/json
```

请求策略：

- 使用 `max_tokens`。
- Generate 使用 `response_format.type = json_object`。
- 按当前 Flint 输出要求设置 `temperature` 和 `thinking`。
- Model 使用 `jsonData.model` 原值，不根据名称改写。

模型目录：

```http
GET https://api.deepseek.com/models
Authorization: Bearer <deepseekApiKey>
```

对于自定义 DeepSeek Base URL，模型地址按以下规则构造：

1. 规范化末尾 `/`。
2. 如果 path 以 `/v1` 结尾，移除 `/v1`。
3. 在同一 scheme 和 host 下追加 `/models`。

模型目录调用失败时返回可操作的清洗错误，配置页继续允许手动 Model ID。

官方依据见 [`provider-model-list-api-research.md`](./provider-model-list-api-research.md)。

## 6. Datasource resources

### 6.1 `GET /health`

只验证已保存配置，不发送 Provider 请求，不产生计费调用。

要求：

- Provider 合法。
- Base URL 合法。
- 当前 Provider Token 非空。
- Model 非空且长度合法。

### 6.2 `GET /models`

使用后端已保存的 Provider、Base URL 和 Token 获取模型目录。

要求：

- Provider 合法。
- Base URL 合法。
- 当前 Provider Token 非空。
- 不要求 Model 已配置。

响应只包含模型 ID：

```json
{
  "models": ["model-a", "model-b"]
}
```

处理规则：

- 验证响应 JSON 结构。
- 提取并 trim `data[].id`。
- 丢弃空值、非法 UTF-8 和超长 ID。
- 去重并稳定排序。
- 限制目录条目数和响应体大小。
- 不透传 Provider 原始 envelope。
- 401、403、429 与普通网络/endpoint 错误分别返回可识别状态。

### 6.2.1 `POST /models/preview`

使用请求体中的 `providerKind`、`baseUrl` 和当前未保存 `apiKey` 临时获取模型目录。该配置仅存在于单次请求内存中，不写回 datasource settings。

要求：

- 仅接受严格、受大小限制的 JSON 请求体。
- 复用与 `GET /models` 相同的 Provider adapter、URL 校验、目录过滤和错误脱敏。
- Token 不进入 URL、日志、响应或浏览器存储。
- Provider HTTP client 拒绝跨 scheme 或 host 重定向，避免 Authorization header 泄漏。
- 仅 Grafana 组织管理员可以调用该临时配置资源。

### 6.3 `POST /test`

使用当前配置发送一个最大 16 输出 token 的最小 chat-completion 请求，以验证 Provider、Base URL、API Key、Model 和实际推理 endpoint。

安全约束：

- 不向浏览器返回模型生成内容，只返回成功状态和固定消息。
- 请求体中的临时 Token 不进入 URL、日志或持久化配置，Provider 错误继续脱敏。
- 请求未携带 Token 时，仅当 Provider 和 Base URL 与已保存配置完全相同时才复用后端已保存 Token，避免把 secret 发送到新 host。
- 该操作是实际 Provider 请求，UI 必须提示可能产生少量计费。
- 仅 Grafana 组织管理员可以调用该临时配置资源。

### 6.4 `POST /chat`

要求完整连接配置和非空 Model。handler 校验输入后调用 `LLMProvider.Chat`，不包含 Provider 分支。

### 6.5 `POST /generate`

要求完整连接配置和非空 Model。handler 校验 Flint 上下文后调用 `LLMProvider.Generate`，不包含 Provider 分支。

Provider 返回不合法结构化结果时，最多允许一次有界纠正请求。

## 7. Base URL 与请求安全

Base URL 必须满足：

- 是包含 scheme 和 host 的绝对 URL。
- 生产配置使用 HTTPS。
- 不包含 username、password、query 或 fragment。
- endpoint 构造保持在配置的 scheme 和 host。
- 仅本地开发环境可通过显式环境变量允许 localhost HTTP。
- 每次连接解析 DNS 后拒绝私有、回环、链路本地和特殊用途地址，并直接拨号到校验后的 IP，避免 DNS rebinding。
- Provider 请求不使用进程级 HTTP proxy，避免代理端重新解析目标地址而绕过出口策略。

安全要求：

1. 已保存 Token 只存在于 `secureJsonData`、后端内存和上游 Authorization header；待保存 Token 仅存在于配置表单和单次 preview/test 请求内存中。
2. 浏览器不接收已保存 Token 明文。
3. 只有 `POST /models/preview` 和 `POST /test` 接受临时 Token；`GET /models`、`/chat`、`/generate` 不接受 Token 字段。
4. 日志、错误响应和测试快照不包含 Token。
5. Provider 响应体、目录条目数、Model ID、请求输入和生成输出均设置上限。
6. CI 和 E2E 只使用本地 stub Provider，不使用真实 Token 或计费请求。
7. Provider 资源按用户限制为每分钟 30 次，并为每个 datasource instance 设置 4 个全局并发槽位。
8. `FLINT_AI_ALLOW_LOCAL_HTTP=true` 仅供本地开发；它允许本地私有网络目标，不得在生产启用。

## 8. Config Editor 交互

配置页包含：

- Provider：OpenAI / DeepSeek 单选。
- Base URL：可编辑输入框。
- API Token：当前 Provider 对应的 `SecretInput`。
- Model：单一可搜索 Combobox；打开时加载目录，同时允许输入任意自定义 Model ID。
- 不提供独立的 Load models 按钮或 From catalog 字段。

模型目录状态：

| 状态            | 行为                                        |
| --------------- | ------------------------------------------- |
| 未保存连接配置  | 使用 `/models/preview` 临时加载目录          |
| Loading         | Combobox 显示 loading，防止重复请求          |
| 成功            | 下拉展示可搜索目录，选择后写入 `jsonData.model` |
| 空目录          | 提示手动输入 Model ID                       |
| 401 / 403       | 提示检查当前 Provider 已保存 Token          |
| 网络失败        | 提示重试或手动输入 Model ID                 |
| endpoint 不支持 | 提示当前 Base URL 无模型目录，保留手动输入  |
| Provider 切换   | 清空目录和错误，恢复目标 Provider 的 Model  |

Grafana resource 请求固定为：

```text
/api/datasources/uid/<uid>/resources/models
/api/datasources/uid/<uid>/resources/models/preview
```

Token 仅出现在 preview POST 请求体，不出现在 URL。

## 9. 实施任务

### T1：配置契约与 Provider 选择

实现：

1. 定义严格的 `openai | deepseek` 前端和后端类型。
2. 定义 `jsonData` 和两个 `secureJsonData` Token 字段。
3. 实现 Provider 默认 Base URL 和切换规则。
4. 删除所有旧名称、旧字段和迁移逻辑。
5. 分离“连接校验”和“完整执行校验”。

完成标准：

- 空值或未知 Provider 明确失败。
- 两个 Provider Token 独立保存和重置。
- Provider 切换遵循第 3.3 节全部规则。
- 单测覆盖配置解析、Token 选择和切换状态。

### T2：Provider 深模块

实现：

1. 建立 `LLMProvider` interface 和 Provider factory。
2. 实现 OpenAI adapter。
3. 实现 DeepSeek adapter。
4. 抽取共享 HTTP transport。
5. 将 `/chat`、`/generate` handler 接入 interface。

完成标准：

- Resource handler 中没有 OpenAI / DeepSeek 条件分支。
- Adapter 测试证明请求路径、header、参数和结构化输出差异。
- 超时、取消、响应上限和 Token 脱敏测试通过。

### T3：模型目录

实现：

1. 注册 `GET /models` 和 `POST /models/preview` resource。
2. 实现 OpenAI 和 DeepSeek 模型 endpoint。
3. 实现模型 envelope 解码、过滤、去重、排序和上限。
4. 实现安全错误映射。
5. 验证 `/models` 不依赖已选 Model。

完成标准：

- 两个 Provider 的标准模型响应均能返回纯 ID 数组。
- 目录失败不修改已保存 Model 或 Token。
- `/models` 测试证明不会访问 `/chat/completions`。
- malformed、oversized、401、403、429、5xx、超时测试通过。

### T4：Config Editor

实现：

1. Provider、Base URL 和双 Token 配置。
2. 支持目录选择和自定义 ID 的单一 Model Combobox。
3. Model 打开时自动加载可搜索目录。
4. 未保存配置 preview 和加载状态反馈。
5. Provider 切换时清理旧目录，并保存/恢复各 Provider 的 Model。

完成标准：

- 用户可以完全不加载目录，直接保存自定义 Model ID。
- 目录加载失败后，自定义 Model ID 和已有配置保持可编辑。
- UI 测试覆盖加载、选择、自定义、切换、错误和 SecretInput reset。
- 浏览器请求和测试快照不包含 Token。

### T5：集成验证与文档

写或修改 E2E 前必须读取 `.config/AGENTS/e2e-testing.md`。

实现：

1. 使用本地 stub Provider 提供 `/models` 和 `/chat/completions`。
2. E2E 覆盖 OpenAI 保存、模型加载、选择和调用。
3. E2E 覆盖 DeepSeek 保存、模型加载、选择和调用。
4. E2E 覆盖自定义 Model ID、错误 Token 和不可用模型 endpoint。
5. 更新 README 和 CHANGELOG。

完成标准：

- 两个 Provider 的核心配置路径在 Grafana 中通过。
- 自定义模型路径通过。
- Stub 证明模型加载不会触发聊天请求。
- 全部质量门禁通过。

## 10. 测试矩阵

| 场景                       | OpenAI        | DeepSeek      |
| -------------------------- | ------------- | ------------- |
| 默认 Base URL              | 必测          | 必测          |
| 自定义 HTTPS Base URL      | 必测          | 必测          |
| 独立 Token 保存/reset      | 必测          | 必测          |
| Chat 请求参数              | 必测          | 必测          |
| Generate 结构化输出        | `json_schema` | `json_object` |
| 模型目录                   | `/v1/models`  | `/models`     |
| 手动 Model ID              | 必测          | 必测          |
| 模型目录不可用后的手动回退 | 必测          | 必测          |
| 401 / 403 / 429 / 5xx      | 必测          | 必测          |
| 超时和响应过大             | 必测          | 必测          |
| Token 不出现在响应/日志    | 必测          | 必测          |

## 11. 质量门禁

以仓库当前脚本为准，至少运行：

```bash
npm run typecheck
npm run lint
npm run test:ci
npm run build
go test ./...
npm run build:backend
npm run e2e
```

前端使用仓库提供的 webpack 配置；后端使用 Grafana Plugin SDK Mage target；E2E 使用 `@grafana/plugin-e2e`。

## 12. 发布验收

以下条件全部满足后才能发布：

1. 只能配置 `openai` 或 `deepseek`，不存在隐式默认或兼容值。
2. 两个 Provider Token 独立加密保存，切换时不会串用。
3. Provider、Base URL、Token 和 Model 在保存后驱动对应后端实例。
4. 模型目录来自后端代理，浏览器不直接连接 Provider；待保存 Token 只发送到同源 preview resource。
5. 用户可以选择目录模型，也可以保存任意非空且长度合法的自定义 Model ID。
6. 模型目录失败不会阻止自定义 Model 或破坏已保存 Token。
7. `/health` 不发送远程或计费请求。
8. `/models` 不触发 `/chat/completions`。
9. 错误、日志、响应和测试产物中不存在 Token。
10. 完整测试矩阵和质量门禁通过。
