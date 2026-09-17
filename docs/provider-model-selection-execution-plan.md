# Flint AI Datasource Provider 与模型选择实现计划

## 1. 目标与范围

保持 `ibumblebee-flint-ai-datasource` 为轻量的 LLM 连接配置插件：用户选择 Provider、填写 API Base URL、保存 API Key，并从该 Provider 当前可用的模型中选择默认模型。首期仅支持 OpenAI 和 DeepSeek。

用户可见流程：

```text
Datasource Settings
  -> Provider: OpenAI / DeepSeek
  -> API Base URL + API Key
  -> Load/Refresh models
  -> 选择模型（可搜索）或手动输入模型 ID
  -> Grafana 保存 datasource 配置
```

范围内：

- Provider 类型仅 `openai`、`deepseek`。
- `baseUrl`、`providerKind`、`model` 继续作为非敏感 datasource `jsonData` 保存。
- API Key 继续放在 `secureJsonData.apiKey`，由 Grafana 加密保存。
- 使用 Provider 官方模型列表 API；调用经 datasource backend 代理，不能从浏览器直连 Provider。
- 对旧 datasource 实例、当前 `/chat`、`/generate` 调用方和已有配置保持兼容。
- 自定义 Base URL 若保留，模型列表获取失败时允许手动输入 Model ID；Provider 类型仍只有 OpenAI/DeepSeek。

不在本计划范围内：

- AI Chat UI、Panel menu/App Modal、Panel JSON Apply、Dashboard 修改。
- 保存动态模型目录到 datasource 配置或把模型列表当成稳定静态枚举。
- OpenAI/DeepSeek 以外 Provider 或通用 Provider marketplace。
- 依据模型名猜测能力（例如推理、工具、图像、结构化输出能力）；模型是否兼容现有请求由 Provider API 实际结果决定。

旧的 Grafana App 实现不属于目标架构。开始本计划的配置实现前，T0 必须先按安全清理要求盘点并清理旧 App 专属实现，保留 datasource 后端、密钥管理、现有 Flint Panel 调用方和可复用资源 API。

## 2. 已有实现与配置契约

当前基础：

- `src/ConfigEditor.tsx` 已有 Provider、Base URL、Model 文本框和 API Key `SecretInput`。
- `src/types.ts` 中 `FlintAiJsonData` 已保存 `providerKind`、`baseUrl`、`model`；`FlintAiSecureJsonData` 保存 `apiKey`。
- `src/configHelpers.ts` 定义 OpenAI/DeepSeek 默认 Base URL，provider 切换时更新默认 URL，并兼容历史 `openai-compatible` 值。
- Go datasource backend 从 `JSONData` / `DecryptedSecureJSONData` 读取配置；当前 resource mux 提供 `/health`、`/chat`、`/generate`。
- `CheckHealth` 与请求处理会验证 Provider、Base URL、Model 和 API Key。

必须遵守：

1. 不改 datasource plugin ID 或 `type: datasource`。
2. Model ID 是配置值，不是 secret；只持久化用户最终选中的单个 ID，不持久化整个动态目录。
3. 已保存的 API Key 不返回浏览器、不写入 `jsonData`、URL、storage、日志或错误信息。
4. Provider 模型查询只经 Grafana resource endpoint 调用 datasource backend；backend 使用当前 datasource 实例保存的 API Key。
5. 配置编辑器中的 API Key 新输入值不能作为 `/models` 请求参数传回 backend。先验证并定义 Grafana 保存凭据后刷新目录的交互；不可为了即时列表把 secret 放入自定义请求 body。
6. 保留自由文本 Model ID 回退，以支持旧实例、Provider API 短暂不可用以及不实现官方 `/models` 的自定义 Base URL。

## 3. 官方模型列表 API 与请求安全

本计划依据的官方 API：

- OpenAI：`GET https://api.openai.com/v1/models`，Bearer API Key，返回模型列表 `data[]`，模型标识为 `id`。见[官方 List models 文档](https://platform.openai.com/docs/api-reference/models/list)和[API 身份验证文档](https://platform.openai.com/docs/api-reference/introduction)。
- DeepSeek：`GET https://api.deepseek.com/models`，Bearer API Key，返回 `{ object: "list", data: [{ id, object, owned_by }] }`。见[官方 Lists Models 文档](https://api-docs.deepseek.com/api/list-models)和[DeepSeek API 认证文档](https://api-docs.deepseek.com/api/deepseek-api)。
- API 核验记录：[`docs/provider-model-list-api-research.md`](./provider-model-list-api-research.md)。

调用规则：

- 默认 endpoint 按 Provider 官方 Base URL 构造；DeepSeek 默认地址应与官方 `https://api.deepseek.com` 对齐。任何默认 URL 变化必须验证现有 `/chat/completions` 调用兼容性。
- OpenAI 当前默认 Base URL 含 `/v1`，其模型 endpoint 为 `<baseUrl>/models`。
- DeepSeek 官方模型列表 endpoint 为 `https://api.deepseek.com/models`；实现必须对当前已保存的 DeepSeek Base URL（包括旧的 `/v1` 后缀）做明确兼容，不能盲目拼接造成 `/v1/models` 或重复 path。兼容规则应由测试覆盖。
- 自定义 Base URL 的 `/models` 兼容性不可假定。列表请求失败时展示清洗后的错误并保留自由文本输入。
- 不硬编码 `gpt-*` / `deepseek-*` 模型清单。Provider 的列表可能包含不适用于现有 `/chat/completions` 请求参数的模型；目录选择不能保证每个模型都能执行每种 Flint 请求。
- 网络超时、响应大小、JSON 结构、模型数量、模型 ID 字节长度均需设上限。只向前端返回必要的 `id`，不透传完整 Provider envelope。

## 4. Cursor Agent 执行约定

### 4.1 单一事实来源与工作区

1. 本文件是 Provider/模型选择功能的执行计划。历史 App Chat 计划不再是新目标的规格。
2. Cursor workspace 同时打开 datasource 仓库和旧 App 同级仓库 `../flint-ai-app`，供 T0 盘点/清理使用。
3. datasource 代码修改前读取 `AGENTS.md` 和 `.config/AGENTS/instructions.md`；修改 E2E 前另读 `.config/AGENTS/e2e-testing.md`。App 仓库遵守其根目录规则。
4. 开始每个任务前分别检查两仓库 Git 状态与完整 diff；清理只针对经证据确认的旧 App 文件，不覆盖用户改动。
5. 一次 Cursor 会话只执行一个任务号；不顺手开始下一任务。

### 4.2 单任务循环

每个任务按顺序完成：

1. 读取本任务、前置交接、相关规则与最小上下文文件。
2. 汇报前置条件、现有改动、计划修改文件、官方 API 依据和验证命令。
3. 先写能证明行为的失败测试，再做最小实现；清理任务先列出准确文件和归属理由。
4. 运行相关单测、typecheck/lint，再运行当前任务要求的完整门禁。
5. 检查完整 diff，更新进度文件并结束当前会话。

推荐启动 Prompt：

```text
执行 @docs/provider-model-selection-execution-plan.md 中的 T<n>，只做该任务。
先读取本任务要求的 AGENTS.md、项目约束和最小上下文；检查 datasource 与旧 App 仓库的 git status。
开始编辑前汇报前置条件、保留的用户改动、计划变更文件、官方 API 证据和验证命令。
按“红灯测试 -> 最小实现 -> 窄验证 -> 完整门禁 -> diff 审阅 -> 更新进度”完成。
API Key 不得进入前端模型列表请求、jsonData、日志或测试产物；不要硬编码动态模型清单。
只执行 T<n>，结束时按计划 8.4 交接。
```

## 5. 分任务执行步骤

### T0：清理旧 App 与确认配置模型查询交互

前置：无。初始上下文：本计划、datasource `AGENTS.md`、`.config/AGENTS/instructions.md`、App `AGENTS.md`、两仓库 Git 状态、[`docs/provider-model-list-api-research.md`](./provider-model-list-api-research.md)、当前 `ConfigEditor` / datasource resource handler / provider config parser。

步骤：

1. 盘点并清理 `../flint-ai-app` 中的 App plugin 注册、Panel menu、Chat Modal、App 页面、App 专属 datasource discovery、相关测试/E2E/依赖/开发挂载/文档。逐文件确认归属；保留用户未提交改动。
2. 保留 datasource 的 Provider 配置、API Key secure storage、`/health`、`/chat`、`/generate`、Flint Panel 调用方和既有配置兼容。
3. 核实 Grafana datasource 配置保存后，`CallResource` 请求是否能使用刚保存的 provider/baseUrl/API key；确认新建实例尚未保存时的可见状态。
4. 设计无 secret 前端转发的交互：使用已保存 datasource backend 配置获取模型目录；如果 provider、Base URL 或 API Key 有未保存更改，提示先保存并刷新/重开配置后加载模型。
5. 确认新建 datasource 在尚未选中 Model 时的保存流程。必要时调整配置校验，让 Model 未选中不会阻止首次保存/加载目录；`/chat`、`/generate` 必须继续拒绝未配置 Model。不得通过静态模型清单满足首次保存。
6. 确认 DeepSeek 默认 URL 和遗留 `/v1` URL 的 `/models` 路径规则，并确认自定义 Base URL 的错误回退行为。
7. 形成短契约：Model 下拉/搜索、刷新、无目录、认证失败、超时、Provider 切换、手动 ID 回退和“配置需先保存”的 UI 文案。

完成标准：旧 App 清理范围有可审阅清单，datasource 共用功能边界明确；新建/已有 datasource 的安全模型加载流程在 Grafana 上可验证；没有把未保存 API Key 发到 backend 的方案。

### T1：后端 Provider 模型目录 resource

前置：T0。上下文：`pkg/plugin/datasource.go`、`pkg/plugin/resources.go`、Provider config、现有 resource tests、官方 endpoint 研究记录。

步骤：

1. 新增只读 resource endpoint（建议 `/models`），由 `Datasource` 实例配置读取 Provider、Base URL 和 decrypted API Key。
2. 对 `GET /models` 做 provider-specific endpoint mapping：OpenAI 与 DeepSeek 路径按官方文档和 T0 遗留 URL 兼容规则处理；不得使用用户输入的任意完整 URL 覆盖 provider endpoint。
3. 仅使用 backend HTTP client 请求 Provider，设置 Bearer header、超时和响应体上限；不把 Authorization header 或 API Key 放进返回体/错误/log。
4. 解码 OpenAI、DeepSeek 的模型列表结构，只提取、去空、校验和稳定排序 `id`；丢弃非法/超长项和不完整对象。
5. 为模型目录验证拆分配置条件：`/models` 需 provider/baseUrl/apiKey，不应依赖已选 `model`；现有聊天/生成请求仍要求非空 model。
6. provider HTTP error 返回安全、可操作的状态/信息；前端获得清洗后的错误，不透传 provider 原文 envelope。
7. `CheckHealth` 只按 T0 决策修改；不为动态模型列表引入不可见的 billable request。

Go 测试：OpenAI/DeepSeek endpoint 和 Bearer header、标准响应提取、空/畸形/超大目录、超时、401/403/429/5xx、secret redaction、无 model 时允许 list 但拒绝 chat/generate、旧配置兼容。

完成标准：模型列表可通过 Grafana datasource resource 获取；API Key 仅驻留 backend 配置/request header；新 endpoint 不改变现有 `/chat`、`/generate` DTO。

### T2：Provider-aware endpoint 构造与 API Key 安全

前置：T1。上下文：`src/configHelpers.ts`、`src/types.ts`、`pkg/plugin/resources.go`、resource client API 和测试。

步骤：

1. 抽取并测试 URL 规范化逻辑，覆盖 OpenAI `/v1`、DeepSeek 官方 root、旧 DeepSeek `/v1`、末尾斜线和不支持的 path。
2. 验证 HTTPS 规则和已有 local HTTP 开发例外；模型 endpoint 不允许 URL credentials/query/fragment，也不得意外跨 host。
3. 确定自定义 Base URL 下 `/models` 失败时返回“可手动输入模型 ID”，不把 OpenAI/DeepSeek 目录错误误显示为凭据错误。
4. 保持服务端读取已保存 secure config 的结构；不要新建接受 `apiKey` 的自定义请求体字段。

完成标准：endpoint 构造有单测保护且无重复 path/Host 逃逸；旧 provider 配置依旧可以调用现有功能。

### T3：Datasource Config Editor 的可搜索模型选择

前置：T0–T2。上下文：`src/ConfigEditor.tsx`、`src/configHelpers.ts`、`src/types.ts`、`src/datasource.ts`、Grafana datasource resource client API 与现有测试。

步骤：

1. 将 Model 文本输入升级为可搜索下拉；使用 backend `/models` 返回的动态 ID 填充，选中值仍保存到 `jsonData.model`。
2. 加入显式 Refresh/Load Models 操作与 loading、empty、auth error、network error、unsupported endpoint 状态。
3. 提供手动输入/保留现有 Model ID 的回退；模型目录失败不能清空已保存模型或阻止 datasource options 继续编辑。
4. Provider 切换时保留用户自定义 Base URL 的既有 helper 语义，清理旧 Provider 的模型列表状态；不要把旧 Provider 的目录显示为当前列表。
5. 凭据、Provider 或 Base URL 未保存时禁用/提示模型刷新；只在 backend 已持有对应已保存 datasource 配置后请求目录。
6. API Key 继续用 `SecretInput`，遵从 `secureJsonFields.apiKey` / reset 语义；不显示已保存明文。
7. UI 说明列表来自 Provider，可用性依赖该 API Key/项目权限；目录项不等同于保证兼容当前所有 Flint 功能。

前端测试：模型加载、搜索/选择持久化、手动 ID 回退、刷新、切换 Provider 清空过期目录、未保存配置拦截、加载错误保留原值、SecretInput/reset 回归、访问性名称和键盘交互。

完成标准：用户能保存 OpenAI/DeepSeek 模型选择；目录加载失败仍可手动配置；已保存 token 不进入前端请求或日志。

### T4：兼容性、集成验证和文档收敛

前置：T0–T3。写或修改 E2E 前读取 `.config/AGENTS/e2e-testing.md`。

步骤：

1. 使用本地 mock HTTP Provider 测试 endpoint 和模型选择，不用真实 token、不访问真实收费聊天接口。
2. E2E 验证：新建 OpenAI datasource、保存 API Key、刷新并选择模型、保存；DeepSeek 同路径；错误 key/离线 Provider；目录失败后手动输入；重开配置时 secret 不可见但模型选择保留。
3. 验证模型目录 `/models` 认证请求未触发 `/chat/completions`，不会产生生成/聊天调用或额外账单路径。
4. 若修改 `src/plugin.json`，重启 Grafana 后验证；若未修改，不做无关 manifest 变更。
5. 更新 datasource README/CHANGELOG，准确描述 OpenAI/DeepSeek 模型发现、API Key 保存、Base URL、手动回退和限制。
6. 将 `AGENTS.md` 的 AI Chat App 说明更新为新的 datasource 配置/模型目录权威计划；旧 App Chat 计划标记为历史/已取代，保留审计记录，不复制旧契约。

完成标准：OpenAI/DeepSeek 配置 E2E 核心路径通过；旧 `/chat`、`/generate` 与 Flint Panel 调用方测试通过；文档只有一个当前目标。

## 6. 测试门禁

### Datasource 仓库

以当前 `package.json` 和 Magefile 中存在的命令为准，至少运行：

```bash
npm run typecheck
npm run lint
npm run test:ci
npm run build
go test ./...
npm run build:backend
```

后端使用 Grafana Plugin SDK 的 Mage target；前端必须使用仓库 webpack 配置。E2E 使用 `@grafana/plugin-e2e`。Provider 测试只连接本地 stub。

### App 仓库清理

T0 记录清理前后 Git 状态和逐文件 diff；删除后运行仍适用的测试/build。不得用 `git clean`、`git reset`、广域递归删除或覆盖用户改动清理 App。

## 7. 发布阻断项

1. 不改 datasource plugin ID/type。
2. 不把 API Key 存在 `jsonData`、浏览器 storage、URL、日志或模型列表响应中。
3. 不从浏览器直接请求 OpenAI/DeepSeek。
4. 不在模型目录 resource 请求中接受前端传来的 API Key。
5. 不将动态模型列表硬编码或持久化；持久化的只有用户所选 model ID。
6. 不把模型列表存在宣称为模型一定支持 `/chat/completions`、JSON mode 或 Flint chart generation。
7. 不因目录服务失败清空现有配置，不让模型列表失败破坏 API Key。
8. 不删除 `/health`、`/chat`、`/generate` 或改坏现有 Flint Panel 调用方。
9. App 清理前必须确认文件属于旧 App，并检查两仓库 Git diff。
10. 不用真实 Provider API Key 或计费聊天请求做 CI/E2E。

## 8. Cursor Agent 交接与进度

### 8.1 任务依赖

```text
T0：清理旧 App + 冻结新配置流程
  |
T1：backend /models
  |
T2：URL / provider endpoint 安全与兼容
  |
T3：Config Editor 动态模型选择
  |
T4：E2E、文档、规则和发布验证
```

### 8.2 每任务初始上下文

| 任务 | 初始上下文 |
| --- | --- |
| T0 | 本计划；两仓库 `AGENTS.md` 和 Git 状态；App plugin manifest/module/tests；`src/ConfigEditor.tsx`；官方模型列表研究文档 |
| T1 | 本计划 T1；`pkg/plugin/datasource.go`、`resources.go`、`provider_adapter.go` 和 resource tests；provider API research |
| T2 | 本计划 T2；`src/configHelpers.ts`、provider config parsing/validation、当前 endpoint tests、research note |
| T3 | 本计划 T3；`src/ConfigEditor.tsx`、`src/datasource.ts`、`src/types.ts`、config helpers 与 Jest tests |
| T4 | 本计划 T4；E2E instructions；Docker/provisioning；README/CHANGELOG；两仓库插件 metadata |

### 8.3 失败修复 Prompt

```text
只修复 T<n> 的验证失败，不扩大功能范围。
先判断失败属于代码、测试、Grafana 版本或环境，并给出证据。
修复后重跑原命令和本任务完整门禁，更新进度，不开始下一任务。
```

### 8.4 进度记录

在 `docs/provider-model-selection-progress.md` 记录状态，仅使用 `未开始`、`进行中`、`已验证`、`阻塞`：

```markdown
## T<n> - <状态> - <日期>

- 完成：<完成标准证据>
- 改动：<仓库与文件>
- 验证：`<命令>` -> PASS/FAIL
- API 证据：<官方链接/endpoint；无则写“无”>
- 决策：<新契约；无则写“无”>
- 未决：<阻塞/风险；无则写“无”>
- 下一步：<唯一后续任务>
```

所有完成标准和对应门禁有可复现证据后才能标为 `已验证`。
