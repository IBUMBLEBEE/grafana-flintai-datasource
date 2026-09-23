# Flint AI Datasource

[English](README.md)

Flint AI Datasource 是一个供 Flint Panel 使用的 Grafana 数据源插件。它安全地保存 AI 服务商配置，并通过 Grafana 后端发送聊天和图表生成请求。

支持的服务商：

- OpenAI
- DeepSeek

![服务商配置](src/img/configuration.png)

## 环境要求

- Grafana 13.1.0 或更高版本
- Node.js 22 或更高版本（仅开发需要）
- Go 1.26.6 或更高版本（仅开发需要）
- Docker 和 Docker Compose（仅本地运行 Grafana 需要）

## 配置数据源

1. 在 Grafana 中添加 **Flint AI Datasource** 数据源。
2. 选择 **OpenAI** 或 **DeepSeek**。
3. 检查默认的 **Base URL**，也可以填写自定义地址。
4. 输入当前服务商的 API Token。
5. 打开 **Model** 并选择模型，或者输入自定义模型 ID 后按 **Enter**。
6. 点击 **Test AI connection** 测试连接。
7. 保存数据源。

OpenAI 和 DeepSeek 的 API Token 与模型选择会分别保存。如果模型列表无法加载，仍然可以手动输入模型 ID。

> **注意：** 测试连接会向服务商发送一个小请求，可能产生少量费用。

## 安全说明

- API Token 保存在 Grafana 的 `secureJsonData` 中。
- 已保存的 Token 不会返回浏览器。
- 浏览器不会直接请求 OpenAI 或 DeepSeek。
- 服务商错误返回浏览器前会进行清理，避免泄露敏感信息。
- 使用未保存配置进行模型预览和连接测试时，必须由 Grafana 组织管理员操作。
- 每次连接都会固定已验证的 DNS 解析结果，并阻止私有、回环、链路本地和特殊用途地址。
- 服务商资源最多允许 4 个并发请求，并限制每位用户每分钟 30 个请求。

服务商请求不会使用进程级 HTTP 代理，避免绕过目标地址校验。仅在本地开发时，`FLINT_AI_ALLOW_LOCAL_HTTP=true` 才会允许访问私有的模拟服务网络；请勿在生产环境中启用该设置。

## 后端资源

| 资源                   | 用途                                   |
| ---------------------- | -------------------------------------- |
| `GET /health`          | 检查已保存的配置，不请求服务商         |
| `GET /models`          | 使用已保存的连接加载模型               |
| `POST /models/preview` | 使用未保存的设置加载模型，但不保存设置 |
| `POST /test`           | 测试服务商连接                         |
| `POST /chat`           | 发送受限的聊天请求                     |
| `POST /generate`       | 生成 Flint 图表方案                    |
| `POST /repair`         | 修复一次被拒绝的图表方案               |

## 本地开发

```bash
npm ci
npm run build
npm run build:backend
npm run server
```

Grafana 会在 <http://localhost:3000> 启动。本地环境还会在 `18080` 端口启动一个模拟 AI 服务，因此端到端测试不需要真实 API Token。

常用命令：

| 命令                     | 用途                            |
| ------------------------ | ------------------------------- |
| `npm run dev`            | 监听并重新构建前端              |
| `npm run typecheck`      | 检查 TypeScript 类型            |
| `npm run lint`           | 运行 ESLint                     |
| `npm run test:ci`        | 运行前端单元测试                |
| `go test ./... -count=1` | 运行后端测试                    |
| `npm run e2e`            | 在本地 Grafana 上运行端到端测试 |

## 发布准备

发布文件准备与创建 Tag 分为两个阶段：

```bash
./scripts/release-pre.sh 0.2.0
git add package.json package-lock.json CHANGELOG.md
git commit -m 'chore(release): v0.2.0'
./scripts/release-pre.sh 0.2.0 --tag
git show v0.2.0
git push origin v0.2.0
```

脚本不会自动提交或推送。创建 Tag 前必须保证 package 与 lockfile 版本一致、Changelog 存在对应版本章节且工作区完全干净。推送 Tag 后会触发带门禁的 GitHub Release workflow；只有前端、后端、漏洞与安全检查全部通过，官方 Grafana Action 才会构建产物并创建 draft release。

## 许可证

Apache-2.0，详见 [LICENSE](LICENSE)。
