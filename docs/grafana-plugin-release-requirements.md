# Grafana datasource plugin 发布与 GitHub Release 要求

> 调研基准日：2026-09-23。本文只引用 Grafana、Grafana 官方 GitHub 组织与 GitHub 官方文档。凡写作“必须”的项目均来自官方政策、元数据规范、提交表单或校验器；“建议”不等同于审核保证。

## 结论先行

1. 面向公众的标准分发渠道是 Grafana Plugin Catalog。GitHub Release 是 Grafana 官方推荐的构建、打包和托管提交产物的路径，但不是提交协议本身的唯一托管方式；提交表单本质上需要一个可公开下载的插件 ZIP URL、对应 SHA1、完整源码 URL 和测试说明。[发布/更新流程](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin) · [GitHub 自动化](https://grafana.com/developers/plugin-tools/publish-a-plugin/build-automation)
2. **首次提交公共插件审核时可以不签名。** 审核通过、Grafana 授予 Community 或 Commercial 签名级别后，才能按公共插件流程签名；正常加载和分发的插件默认必须有有效签名，签名结果是包内的 `MANIFEST.txt`。[签名流程](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin) · [发布 FAQ](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-faqs)
3. 本项目连接 OpenAI 与 DeepSeek 商业 API。根据政策中“商业依赖或由商业实体支持的技术”归入 Commercial 的文字，**本插件很可能需要 Commercial 分类及 Commercial Plugin Subscription**；这是依据政策作出的推断，最终分类由 Grafana 个案决定。应在打正式 tag 前联系 `integrations@grafana.com` 确认，不能自行假定为免费的 Community 插件。[Grafana Plugins Policy](https://grafana.com/legal/plugins/)
4. GitHub 自动生成的 “Source code (zip/tar.gz)” 是 tag 对应的源码快照，不是 Grafana 插件安装包。提交时必须使用构建流程额外上传的、以插件 ID 为 ZIP 根目录且包含编译产物的 release asset。[GitHub Release 说明](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases) · [Grafana 打包格式](https://grafana.com/developers/plugin-tools/publish-a-plugin/package-a-plugin)

本文采用以下分级：

| 级别         | 含义                                                                               | 例子                                                            |
| ------------ | ---------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| 硬要求       | 官方政策、包格式、元数据、提交表单或审核规则要求；不满足会阻止提交、加载或通过审核 | 许可、公开源码、ZIP 结构、SHA1、签名（首次审核例外）、版本递增  |
| 官方工具约束 | 选择官方 GitHub Action 后必须满足，否则 workflow 失败                              | `v${version}` tag、所需 Actions 权限、Action 支持的 Mage target |
| 最佳实践     | 官方强烈建议，用于降低缺陷和缩短审核，但不保证获批                                 | provisioning、E2E 版本矩阵、完整 CHANGELOG、attestation         |
| 项目判断     | 依据本仓库事实和政策作出的推断，需负责人或 Grafana 确认                            | OpenAI/DeepSeek 依赖可能导致 Commercial 分类                    |

## 1. 发布路径与顺序

### 首次公共插件提交

1. 先确认 Community、Commercial 或 Marketplace 分类，以及许可和订阅条件。[分类与许可](https://grafana.com/legal/plugins/)
2. 完成元数据、README、CHANGELOG、测试环境及质量检查。
3. 构建前端和全部拟支持平台的后端二进制，将二进制权限设为 `0755`。[打包说明](https://grafana.com/developers/plugin-tools/publish-a-plugin/package-a-plugin)
4. 首次审核包允许无签名；不要为绕过公共插件审核而使用 private `rootUrls` 签名。[公共/私有签名流程](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin)
5. 将 `dist` 重命名为插件 ID 后压成 ZIP；运行官方 Plugin Validator。[打包说明](https://grafana.com/developers/plugin-tools/publish-a-plugin/package-a-plugin) · [Plugin Validator](https://github.com/grafana/plugin-validator)
6. 建立与版本一致的 tag，让官方 Action 生成插件 ZIP、`.sha1` 和 draft GitHub Release；检查校验报告与 release notes 后再公开发布 draft。[官方 Action 文档](https://github.com/grafana/plugin-actions/blob/build-plugin/v1.2.0/build-plugin/README.md)
7. 由目标 Grafana Cloud organization 的管理员在 **Org Settings → My Plugins → Submit New Plugin** 提交公开可下载的 release asset。[提交表单](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin)
8. 审核包括自动校验、人工源码/安全审查和安装后的基本功能测试；提供可复现环境能加速，但不保证通过。[审核流程](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin) · [测试环境](https://grafana.com/developers/plugin-tools/publish-a-plugin/provide-test-environment)

### 审核通过后的公共版本

获得签名级别后，创建 realm 为所属 Grafana Cloud organization（`all-stacks`）、scope 为 `plugins:write` 的 Access Policy token，并把它作为 `GRAFANA_ACCESS_POLICY_TOKEN` 保存到 GitHub Actions secret。此后由官方 Action 在打包前签名，ZIP 内必须含 `MANIFEST.txt`；签名后不得再修改包内任何文件。[Access Policy token 与签名](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin)

更新已有 catalog 插件时使用 **Submit Update**。更新与首次提交一样会经历自动和人工审核；提交版本必须高于 catalog 中的现有版本。[更新流程](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin) · [Validator 的 version 检查](https://github.com/grafana/plugin-validator#analyzers)

## 2. 分类、许可、隐私与安全硬要求

- Catalog 插件许可必须是 `AGPL-3.0`、`Apache-2.0`、`BSD`、`GPL-3.0`、`LGPL-3.0` 或 `MIT` 之一；Marketplace 可采用专有许可。本仓库的 `Apache-2.0` 在允许列表内。[Accepted licenses](https://grafana.com/legal/plugins/#accepted-licenses)
- Community 插件还必须已有公开 Git 仓库，且其依赖技术可供 Grafana 测试；Commercial 插件需要相应订阅。[签名分类与前置条件](https://grafana.com/legal/plugins/#how-to-sign-a-plugin)
- 不得加入安装量、用户行为或第三方分析追踪；敏感数据必须按行业标准加密，datasource 凭据使用 `secureJsonData`，不得存入 panel options；传输使用 TLS/SSL。[Privacy and security](https://grafana.com/legal/plugins/#privacy-and-security)
- 不得包含隐藏文件，不得超出功能范围操纵宿主环境、权限或进程。[Privacy and security](https://grafana.com/legal/plugins/#privacy-and-security)
- 第三方依赖必须有合法使用权并清楚署名；嵌入的 logo 和商标也必须有使用权。[Right to use and proper crediting](https://grafana.com/legal/plugins/#right-to-use-and-proper-crediting)
- Fork、已有插件的衍生/重复实现、单包嵌多个插件、只适用于作者特定环境或对社区价值过窄，都可能被拒绝；最终是否发布由 Grafana 个案决定。[Restrictions](https://grafana.com/legal/plugins/#restrictions)

## 3. `plugin.json` 与版本

所有插件都必须带有效 `plugin.json`。发布前至少核对以下内容：[plugin.json 官方参考](https://grafana.com/developers/plugin-tools/reference/plugin-json)

- `id` 唯一，并匹配 `^[0-9a-z]+-([0-9a-z]+-)?(app|panel|datasource)$`；类型为 `datasource` 时 ID 以 `-datasource` 结束。
- `type`、`name`、`info`、`dependencies` 存在；backend 插件的 `backend: true` 与 `executable` 和实际二进制一致。
- `info.version` 是 SemVer；`info.updated`、`info.keywords`、`info.logos` 完整。
- `info.links` 必须有实际链接。官方参考特别说明，留空会被要求重新提交；通常至少提供项目主页、源码或问题反馈入口。
- `dependencies.grafanaDependency` 是真实测试过的兼容范围，不能为了通过校验而无依据放宽。
- `package.json.version`、构建后 `dist/plugin.json.info.version` 与 release 版本一致。官方 Validator 会检查 `package.json`/`plugin.json` 版本一致性以及更新版本是否高于 catalog 已发布版。[Validator analyzers](https://github.com/grafana/plugin-validator#analyzers)

若沿用 Grafana 官方 `build-plugin` Action，tag 必须严格等于 `v${pluginVersion}`，例如本项目首版 `0.1.0` 对应 tag `v0.1.0`；不一致时 Action 会失败。[Action 的版本检查](https://github.com/grafana/plugin-actions/blob/build-plugin/v1.2.0/build-plugin/action.yml#L82-L87)

## 4. CHANGELOG、README 与 Release Notes

`README.md` 和 `CHANGELOG.md` 应进入插件 ZIP；官方 Validator 分别检查两者是否存在。Catalog 详情页内容取自包内 README，因此 README 变化也需要提交新插件版本。[Validator analyzers](https://github.com/grafana/plugin-validator#analyzers) · [发布 FAQ](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-faqs)

Grafana 对 CHANGELOG 的官方最佳实践是：[Publishing best practices](https://grafana.com/developers/plugin-tools/publish-a-plugin/publishing-best-practices)

- 仓库根目录使用独立 `CHANGELOG.md`；
- 按 `MAJOR.MINOR.PATCH` 版本组织，并为每个 release 写日期；
- 按 Features、Bug Fixes、Breaking Changes 等类型归组；
- 链接相关 PR；
- 显著说明破坏性变更及用户需要采取的动作。

GitHub 本身允许手写或自动生成 release notes；Grafana 官方 Action 会创建 draft release 并启用 GitHub 自动 release notes。发布 draft 前应删掉 Action 的操作提示，改成面向用户的版本摘要，并与 CHANGELOG 保持一致。[GitHub release notes](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases) · [Grafana Action 创建 draft release](https://github.com/grafana/plugin-actions/blob/build-plugin/v1.2.0/build-plugin/action.yml#L117-L148)

## 5. 构建、签名和 ZIP 结构

官方手工流程是：安装依赖并构建前端；backend 插件再用 Mage 构建；签名；把 `dist` 重命名成插件 ID；递归创建 ZIP。[Package a plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/package-a-plugin)

产物检查应满足：

- ZIP 只有一个顶层目录，目录名精确等于 `ibumblebee-flintai-datasource`，而不是 `dist`。
- 顶层目录内直接包含 `plugin.json`、`module.js`、README、CHANGELOG、图片和后端二进制；不得多套一层目录。
- `plugin.json.executable` 与后端文件前缀一致，文件名遵循 `<executable>_<GOOS>_<goarch>`（Windows 加 `.exe`）。[backend executable 命名](https://grafana.com/developers/plugin-tools/reference/plugin-json#properties)
- 所有后端二进制在 ZIP 中保留 `0755` 可执行权限。[Package a plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/package-a-plugin)
- 已获公共签名级别的产物包含 `MANIFEST.txt`；其中记录插件元数据和各文件 SHA256，并带数字签名。[MANIFEST 结构](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin#add-a-plugin-manifest-for-verification)
- release asset 文件名宜采用官方 Action 的 `<plugin-id>-<version>.zip`，并同时发布其 `.zip.sha1`。[官方 package-plugin Action](https://github.com/grafana/plugin-actions/blob/main/package-plugin/action.yml)

不要在签名后重新压入、格式化或修改文件；任何签名清单未覆盖或 checksum 改变的内容都会使签名失效。[签名验证说明](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin#add-a-plugin-manifest-for-verification)

## 6. GitHub Release 与官方工作流

Grafana 官方推荐在 `v*` tag push 时运行 `grafana/plugin-actions/build-plugin`。它会安装依赖、构建并测试前端、执行 backend coverage 和 Mage 构建、按条件签名、打包、运行 Plugin Validator、生成 SHA1，并创建 **draft** GitHub Release。[自动化指南](https://grafana.com/developers/plugin-tools/publish-a-plugin/build-automation) · [package-plugin 实现](https://github.com/grafana/plugin-actions/blob/main/package-plugin/action.yml)

官方 released action 示例使用 `grafana/plugin-actions/build-plugin@build-plugin/v1.2.0`。本项目声明 Node `>=22`、Go `1.26.6`，而该 Action 默认值分别为 Node 20、Go 1.25，因此工作流必须显式设置 `node-version: '22'` 与 `go-version: '1.26.6'`。backend 的默认 target 是 `buildAll`；发布矩阵与 Mage targets 必须匹配。[Action inputs](https://github.com/grafana/plugin-actions/blob/build-plugin/v1.2.0/build-plugin/action.yml#L12-L52)

参考形态如下；首次公共审核尚未获签名级别时，不配置 `policy_token`，获批后再启用 secret：

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      id-token: write
      attestations: write
    steps:
      - uses: actions/checkout@v4
      - uses: grafana/plugin-actions/build-plugin@build-plugin/v1.2.0
        with:
          node-version: '22'
          go-version: '1.26.6'
          backend-target: buildAll
          policy_token: ${{ secrets.GRAFANA_ACCESS_POLICY_TOKEN }}
          attestation: true
```

`contents: write` 用于建立 GitHub Release；启用 build provenance attestation 时还需 `id-token: write` 与 `attestations: write`。Attestation 是供应链增强项，并由 Validator 的 provenance analyzer 校验，但 Grafana 的提交表单没有把它列为必填字段。[Action attestation 文档](https://github.com/grafana/plugin-actions/blob/build-plugin/v1.2.0/build-plugin/README.md#attestation-of-plugin-package) · [Validator analyzers](https://github.com/grafana/plugin-validator#analyzers)

如果仓库启用 GitHub immutable releases，应先创建 draft、把最终资产全部附上后再发布；发布后 tag 与 assets 会锁定。它是 GitHub 的可选供应链保护，不是 Grafana Catalog 的硬要求。[GitHub immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)

## 7. Plugin Validator

提交前必须把 Validator 的所有 error 清零，并逐项审阅 warning；Grafana 提交时会用自己的配置再次自动校验，本地自定义规则不能改变服务端审核规则。[Plugin Validator](https://github.com/grafana/plugin-validator)

推荐对最终 release 候选包执行：

```bash
npx -y @grafana/plugin-validator@latest \
  -sourceCodeUri file://. \
  ibumblebee-flintai-datasource-0.1.0.zip
```

正式 GitHub Release 后，再用与提交完全相同的公开 URL 和 tag 固定源码复验：

```bash
docker run --pull=always grafana/plugin-validator-cli \
  -sourceCodeUri https://github.com/OWNER/REPO/tree/v0.1.0 \
  https://github.com/OWNER/REPO/releases/download/v0.1.0/ibumblebee-flintai-datasource-0.1.0.zip
```

Validator 覆盖 archive 名称/结构、backend 声明与二进制、可执行权限、README、CHANGELOG、许可、元数据、签名、版本、源码与二进制一致性、source map、SDK 使用、React 兼容性、恶意代码、漏洞和 provenance 等检查；提供 tag 固定的 `sourceCodeUri` 才能运行完整的源码相关分析。[Analyzer 清单](https://github.com/grafana/plugin-validator#analyzers) · [源码 URL 要求](https://github.com/grafana/plugin-validator#source-code)

## 8. 提交表单材料

由 Grafana Cloud organization 管理员准备并核对：[Publish or update a plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin)

- **OS & Architecture**：单个多架构 ZIP 选 Single；按架构分别提供 ZIP 选 Multiple。选择必须与实际资产一致。
- **URL**：公开、无需登录、长期稳定、直接返回最终插件 ZIP 的 GitHub Release asset URL。
- **Source code URL**：完整公开源码，推荐固定到同一 release tag，例如 `https://github.com/OWNER/REPO/tree/v0.1.0`；不要用不断变化的默认分支。[支持的 URL 形式](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-faqs#what-source-code-url-formats-are-supported)
- **SHA1**：最终 ZIP 的 SHA1；必须对 URL 实际下载的字节计算。官方 Action 同时生成 `.zip.sha1` asset。[提交字段](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin) · [Action outputs](https://github.com/grafana/plugin-actions/blob/build-plugin/v1.2.0/build-plugin/action.yml#L4-L9)
- **Testing guidance**：覆盖安装、配置和使用；本插件还应明确如何准备 OpenAI/DeepSeek 测试凭据、可能产生的 API 费用、可验证的健康检查/模型发现/连接测试行为，以及不在日志或表单中泄露 token 的操作方式。
- **Provisioning provided**：只有确实提供可用 provisioning 时才勾选。
- **分类问题**：如实描述商业 API 依赖及发行者身份，供 Grafana 确定 Community/Commercial/Marketplace 级别。

SHA1 字段存在一处官方表述差异：发布文档称填写 ZIP 的 SHA1 hash，而官方 Action 生成的 draft release 提示粘贴 `.zip.sha1` asset link。实际提交时应以当时 Grafana Cloud 表单的字段说明为准，并同时保留**纯 hash 值**和**公开 `.sha1` URL**，不可把两者混淆。[提交文档](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin) · [Action draft 文案](https://github.com/grafana/plugin-actions/blob/build-plugin/v1.2.0/build-plugin/action.yml#L130-L141)

## 9. 可复现测试环境

Provisioning 不是提交硬门槛，但官方明确表示它能显著缩短审查。简单 datasource 通常应提供测试 dashboard、可直接启动的 `docker-compose.yml` 和完整配置；更复杂的插件可能还需测试 API。理想状态是在全新环境执行 `docker compose up` 就能看到可工作的示例。[Help us test your plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/provide-test-environment)

E2E 应覆盖声称支持的 Grafana 版本并进入 CI；这是官方发布最佳实践，而非审批保证。[Publishing best practices](https://grafana.com/developers/plugin-tools/publish-a-plugin/publishing-best-practices#end-to-end-testing)

## 10. 发布前逐项 Checklist

### 分类与合规

- [ ] 已向 Grafana 确认 OpenAI/DeepSeek 依赖对应的签名分类和订阅要求。
- [ ] 公开仓库、许可证、第三方依赖署名、logo/商标权利均无问题。
- [ ] 无 analytics/tracking；秘密只存于 `secureJsonData`；所有生产上游传输使用 TLS。
- [ ] 包内没有隐藏文件、调试文件、测试密钥或本地配置。

### 元数据与文档

- [ ] 首发前最终 `id`/`type` 已定稿且合规，发布后不再变更；backend、executable、依赖范围与产物一致。
- [ ] `info.links`、keywords、logos、description、author、updated 均可用于 catalog。
- [ ] `package.json.version`、构建后 `plugin.json.info.version`、CHANGELOG 和 tag 完全一致。
- [ ] CHANGELOG 有当前版本、发布日期、分类变更和 breaking change 说明。
- [ ] README 面向最终用户，含安装、配置、使用、数据流/第三方 API、支持和反馈入口。

### 质量与安全

- [ ] lint、typecheck、前后端单元测试和 E2E 全部通过。
- [ ] 在声明的最低及当前支持 Grafana 版本做过 E2E/人工 smoke test。
- [ ] 依赖与 Go backend 无已知不可接受漏洞；Grafana SDK 与官方工具没有过时警告。
- [ ] 最终 ZIP 的 Plugin Validator 无 error，warning 已逐条审阅和处置。
- [ ] 从公开 tag 与 release URL 再跑一次 Validator，结果与本地一致。

### 包与签名

- [ ] ZIP 顶层目录精确为插件 ID，内含 README、CHANGELOG、`plugin.json`、前端产物和正确平台二进制。
- [ ] `zipinfo` 显示所有 backend binaries 为 `0755`。
- [ ] 首次审核包按首次提交例外处理；已获签名级别的公共包内有有效 `MANIFEST.txt`。
- [ ] 签名后未修改任何包内文件。
- [ ] ZIP SHA1 已从最终字节重新计算并与 `.sha1` asset 一致。

### GitHub Release

- [ ] release workflow 使用正确 Node、Go 与 backend target，所需权限最小且完整。
- [ ] tag 为 `vMAJOR.MINOR.PATCH`，并指向要审核的准确 commit。
- [ ] workflow、测试、Validator、签名（适用时）和 attestation（启用时）全部成功。
- [ ] draft 中已上传插件 ZIP 和 `.zip.sha1`，release notes 已整理为用户可读内容。
- [ ] 发布后两个 asset URL 均可在未登录状态直接下载；没有误用 GitHub 自动源码 ZIP。
- [ ] 若启用 immutable release，发布前已确认 tag 和所有 assets 最终无误。

### Grafana 提交

- [ ] 操作者是对应 Grafana Cloud organization 的管理员。
- [ ] OS/Architecture 选择与 ZIP 内二进制矩阵一致。
- [ ] Plugin URL、tag 固定的公开源码 URL、SHA1 和 testing guidance 已备齐。
- [ ] 测试凭据/测试 API/provisioning 能让审核人员在不联系作者的情况下完成基本验证，或 testing guidance 明确说明必要协作。
- [ ] 已理解审核完成时间与发布日期不能预先保证，且批准是个案决定。[发布 FAQ](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-faqs)

## 官方来源索引

- [Grafana Plugins Policy（分类、许可、隐私、安全）](https://grafana.com/legal/plugins/)
- [Publish or update a plugin（提交字段与审核流程）](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin)
- [Package a plugin（构建与 ZIP 结构）](https://grafana.com/developers/plugin-tools/publish-a-plugin/package-a-plugin)
- [Sign a plugin（首次例外、token、签名与 MANIFEST）](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin)
- [Publishing best practices（README、CHANGELOG、E2E、Validator）](https://grafana.com/developers/plugin-tools/publish-a-plugin/publishing-best-practices)
- [Build automation（官方 GitHub Actions release 流程）](https://grafana.com/developers/plugin-tools/publish-a-plugin/build-automation)
- [Plugin metadata reference](https://grafana.com/developers/plugin-tools/reference/plugin-json)
- [Grafana Plugin Validator](https://github.com/grafana/plugin-validator)
- [Grafana plugin-actions](https://github.com/grafana/plugin-actions)
- [GitHub releases](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases)
- [GitHub immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)
