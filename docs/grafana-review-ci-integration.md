# GitHub CI 与 Grafana Plugin 审核流程集成

调研日期：2026-09-23；Access Policy scope 复核：2026-09-24。本文只使用 Grafana、Grafana 官方 GitHub 仓库与 GitHub 官方文档。

## 结论

当前失败不是 GitHub Actions 权限或签名配置问题，而是插件 ID 的 organization 前缀没有对应的 Grafana Cloud organization：

- `unsigned plugin` 对首次提交的新公共插件是预期 warning，可以带着它继续初审。Grafana 明确说明开发阶段和第一次提交审核时不需要签名；公共插件须先经 Grafana 审核并获得签名级别，之后才能签名。[Grafana：Sign a plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin#signatures-during-plugin-development)
- `unregistered Grafana Cloud account: ibumblebee` 是必须解决的问题。Validator 取插件 ID 第一个 `-` 之前的部分作为 organization slug；当前 `ibumblebee-flintai-datasource` 因而要求存在 slug 为 `ibumblebee` 的 Grafana Cloud organization。[Validator `org` analyzer](https://github.com/grafana/plugin-validator/blob/8664ce2507e3b342ad3125853e96e2315516c757/pkg/analysis/passes/org/org.go#L43-L68) Grafana 的创建工具同样要求插件 ID 使用 Grafana Cloud organization 名称，并说明该名称用于签名和发布。[Grafana CLI reference](https://grafana.com/developers/plugin-tools/reference/cli-commands#enter-your-organization-name-usually-your-grafana-cloud-org)
- `Scan incomplete: organization not found` 不是第三个独立问题。`org` analyzer 报出未注册 warning 后会返回 `ErrOrganizationNotFound`；runner 将 analyzer 的执行错误再记录为 `Scan incomplete` error。因此注册/改正 organization 后，两条 organization 相关诊断应一起消失。[Validator `org` analyzer](https://github.com/grafana/plugin-validator/blob/8664ce2507e3b342ad3125853e96e2315516c757/pkg/analysis/passes/org/org.go#L54-L68) · [Validator runner](https://github.com/grafana/plugin-validator/blob/8664ce2507e3b342ad3125853e96e2315516c757/pkg/runner/runner.go#L90-L103) · [`ReportIncomplete`](https://github.com/grafana/plugin-validator/blob/8664ce2507e3b342ad3125853e96e2315516c757/pkg/analysis/incomplete.go#L3-L21)

本仓库的处置是：创建或确认 slug 精确为 `ibumblebee` 的 Grafana Cloud organization，并确保提交人是该 organization 的管理员。不要通过 CI 屏蔽该检查，也不要为此修改本仓库的插件 ID；如果 `ibumblebee` slug 无法创建或无法归属到正确账号，应在首次发布前联系 Grafana 解决 organization 归属。Grafana 的发布入口要求使用待发布 organization 的管理员账号。[Grafana：Publish or update a plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin#publish-your-plugin)

## 2026-09-24：Access Policy scope 更正

`stack-plugins:write` **不是**签名所需 `plugins:write` 的替代项或别名。Grafana 当前的 Access Policy 配置 API 将二者列为不同 scope：`plugins:write` 的说明是管理 Grafana.com 上的插件，并明确要求 `org` realm；`stack-plugins:write` 的说明则是管理某个 Grafana Cloud stack 中的插件安装。官方 Terraform 指南也只把 `stack-plugins:{read,write,delete}` 用于安装、更新或删除 stack 上的 catalog plugin。[Grafana Access Policy scope 配置](https://grafana.com/api/v1/accesspolicies/config?region=us) · [Grafana Cloud plugin installation](https://grafana.com/docs/grafana/latest/as-code/infrastructure-as-code/terraform/terraform-plugins/)

当前 UI 的正确选择是：Realm 选 **`ibumblebee (all-stacks)`**，然后在 **Additional scopes** 中搜索并选择字面值 **`plugins:write`**；不要勾选 `stack-plugins` resource 的 Write 来代替。Grafana 当前签名文档和配图仍明确展示 organization/all-stacks realm 与 `plugins:write`。[签名文档源码](https://github.com/grafana/plugin-tools/blob/67f6fec1321943ae8062e130745b37f3e45c1d72/docusaurus/docs/publish-a-plugin/sign-a-plugin.md#L26-L49) · [官方 Access Policy 配图](https://github.com/grafana/plugin-tools/blob/67f6fec1321943ae8062e130745b37f3e45c1d72/docusaurus/website/static/img/create-access-policy-v2.png)

Access Policy token 仍在普通 Grafana Cloud Access Policies 页面创建，不存在另一个 public-plugin signing token 入口。审批控制的是插件的 **public signature level**，不是 token 是否可以创建：token 可以预先创建，但首次公开送审包仍应保持 unsigned；只有 Grafana 审核通过并授予签名级别后才使用它做 public signing。`@grafana/sign-plugin` 会把这个 Bearer token 原样发送到 `https://grafana.com/api/plugins/ci/sign`，客户端没有把 `stack-plugins:write` 转换为 `plugins:write` 的逻辑。[Public plugin 审核与签名顺序](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin#sign-a-public-plugin) · [`sign-plugin` 请求实现](https://github.com/grafana/plugin-tools/blob/67f6fec1321943ae8062e130745b37f3e45c1d72/packages/sign-plugin/src/utils/manifest.ts#L88-L100)

如果以 organization 管理员身份选择 `ibumblebee (all-stacks)` 后，Additional scopes 中仍没有 `plugins:write`，不要改用 `stack-plugins:write`。应把 organization slug 和 UI 截图交给 Grafana Plugins reviewer 或 Grafana Support，确认 organization realm/权限或修正文档与 UI 的不一致。

## CI 和人工审核的职责边界

推荐把流程分成四层：

| 阶段           | GitHub CI 负责                                                                                                                         | 人工/Grafana 负责                                                                                                              |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| PR / 普通 push | lint、类型检查、单元测试、backend 测试、构建、E2E、快速元数据检查                                                                      | 审阅代码和测试结果                                                                                                             |
| release tag    | 从固定 tag 构建全部平台产物；按插件 ID 打包；运行完整 Validator；生成 ZIP、SHA1、GitHub Release；公共仓库可生成 provenance attestation | 检查最终 release asset、说明和匿名下载                                                                                         |
| 首次提交       | CI 可以产出未签名包；完整 Validator 只应容忍首次未签名 warning，不能容忍 organization error                                            | organization 管理员在 **Org Settings → My Plugins → Submit New Plugin** 填写 ZIP URL、tag 固定的源码 URL、SHA1、架构和测试说明 |
| 审核通过后     | 使用 GitHub secret 中的 Access Policy token 自动签名后再打包；后续版本继续由完整 Validator 门禁                                        | Grafana 授予 Community/Commercial 签名级别；每次提交仍会做自动和人工审核                                                       |

Grafana 官方文档中的自动化终点是生成可提交的 GitHub Release ZIP 和 SHA1；正式发布仍通过 Grafana Cloud 的提交表单。提交后先运行自动校验，再进入人工源码、安全和安装测试。[Grafana：build automation](https://grafana.com/developers/plugin-tools/publish-a-plugin/build-automation#use-your-release-assets-for-your-plugin-submission) · [Grafana：plugin submission review](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin#plugin-submission-review)

## 本仓库现状

本仓库已经具备正确的总体结构，但两个 workflow 的检查深度不同：

1. `.github/workflows/ci.yml` 的 `Validate plugin metadata` 只运行 `-analyzer=metadatavalid`。它适合作为普通 CI 的快速检查，但不会运行 `org` analyzer，所以不能提前发现本次 organization 问题。
2. `.github/workflows/release.yml` 使用固定 SHA 的 `grafana/plugin-actions/build-plugin`。该 Action 内部的 `package-plugin` 会构建、按条件签名、打包，并以 `-sourceCodeUri file://./` 运行完整 Plugin Validator；Validator 成功后才继续创建 GitHub draft release。[官方 `package-plugin` 实现](https://github.com/grafana/plugin-actions/blob/8b12a2d6fa5fc0939a6bf75905453d4b505ef29a/package-plugin/action.yml#L92-L145) 因此当前 organization error 正确地阻止 release，不应降级或屏蔽。
3. release job 已有 `contents: write`、`id-token: write`、`attestations: write`，并仅在公开仓库启用 attestation，符合官方 Action 所需权限与 Grafana 的公开仓库限制。[Grafana：provenance attestation](https://grafana.com/developers/plugin-tools/publish-a-plugin/build-automation#attest-provenance-for-plugin-builds)
4. workflow 已把 `secrets.GRAFANA_ACCESS_POLICY_TOKEN` 传给 Action。GitHub 对未配置的 secret 求值为空字符串；官方 `package-plugin` 只在 token 非空时执行签名。因此首次审核阶段未设置该 secret 时会生成未签名包，这是可接受的预期行为。[GitHub：Using secrets](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets#using-secrets-in-a-workflow) · [官方 `package-plugin` 签名条件](https://github.com/grafana/plugin-actions/blob/8b12a2d6fa5fc0939a6bf75905453d4b505ef29a/package-plugin/action.yml#L92-L112)
5. `build-plugin` 创建 draft release 后，当前 workflow 会立即执行 `gh release edit --draft=false`。这不是本次失败的原因，但 Grafana 官方流程是先人工检查 draft 的说明和 assets，再发布并用该 assets 提交审核。若要把审核关口反映在 CI 中，可移除自动发布步骤，或将它放在需要审批的 GitHub Environment 后。[Grafana：Publish your release in GitHub](https://grafana.com/developers/plugin-tools/publish-a-plugin/build-automation#publish-your-release-in-github)

当前不需要通过修改 workflow 来修复这三条消息。若希望更早反馈，可以以后在普通 CI 中增加完整 Validator job；但 organization 的真正修复仍然是注册/选择正确的 Grafana Cloud organization，而不是禁用 analyzer。Validator 文档也注明，提交服务使用 Grafana 自己的配置，本地配置无法改变服务端规则。[Grafana Plugin Validator configuration](https://github.com/grafana/plugin-validator#enabling-and-disabling-analyzers-via-config)

## 可执行顺序

### 第一次审核前

1. 在 Grafana Cloud 创建或确认 organization slug 为 `ibumblebee`；确认提交人具有该 organization 的管理员权限。
2. 等 organization 可被 Validator 查询后，重新运行失败的 release workflow。GitHub 支持在原始事件后 30 天内重跑，并保持原来的 `GITHUB_SHA` 与 `GITHUB_REF`，因此不需要为了外部 organization 状态变化而制造一个新 commit 或移动 tag。[GitHub：Re-running workflows and jobs](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/re-run-workflows-and-jobs)
3. 要求完整 Validator 没有 error。此时仅保留内容明确为“new (unpublished) plugin”的 unsigned warning 是正常的；Validator 自身就是通过“catalog 中未发布”来选择这段初审提示。[Validator `manifest` analyzer](https://github.com/grafana/plugin-validator/blob/8664ce2507e3b342ad3125853e96e2315516c757/pkg/analysis/passes/manifest/manifest.go#L58-L74)
4. 确保仓库与 release asset 可匿名访问，核对 ZIP 和 `.sha1`，然后由 organization 管理员在 Grafana Cloud 手工提交。表单需要插件 ZIP URL、完整源码 URL、SHA1、架构选择和 testing guidance。[Grafana：submission fields](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin#publish-your-plugin)
5. 等待 Grafana 的自动校验和人工审核；不要使用 private-plugin `rootUrls` 签名来绕过公共插件的首次审核顺序。

### 获得公共签名级别后

1. 在同一个 Grafana Cloud organization 创建或复用 Access Policy：Realm 选择 `ibumblebee (all-stacks)`，在 **Additional scopes** 中选择 `plugins:write`，不要使用 `stack-plugins:write`。[Grafana：Generate an Access Policy token](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin#generate-an-access-policy-token)
2. 把 token 保存为 GitHub repository Actions secret `GRAFANA_ACCESS_POLICY_TOKEN`；GitHub UI 路径为 **Settings → Secrets and variables → Actions → New repository secret**，也可执行 `gh secret set GRAFANA_ACCESS_POLICY_TOKEN`。[GitHub：Creating secrets for a repository](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets#creating-secrets-for-a-repository)
3. 按 Grafana reviewer 的指示重新构建需要签名的候选包。之后的 tag release 会因 token 非空而在打包前自动生成 `MANIFEST.txt`；token 所属 organization 必须与插件 ID 的第一段一致。[Grafana：Sign a public plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin#sign-a-public-plugin)
4. 后续更新使用 **Submit Update**，仍会经历相同的自动和人工审核。[Grafana：Update your plugin](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin#update-your-plugin)

## 当前三条诊断的验收标准

| 诊断                                                      | 初次提交能否接受                                | 完成条件                                                                        |
| --------------------------------------------------------- | ----------------------------------------------- | ------------------------------------------------------------------------------- |
| `warning: unsigned plugin`                                | 可以，仅限消息明确说明是 new/unpublished plugin | 首次审核继续；Grafana 授予签名级别后，后续/指定候选包必须含有效 `MANIFEST.txt`  |
| `warning: unregistered Grafana Cloud account: ibumblebee` | 不应接受                                        | Grafana Cloud 存在 slug 为 `ibumblebee` 的 organization，且提交人具有管理员权限 |
| `error: Scan incomplete — organization not found`         | 不可以                                          | 修复上一项后完整 Validator 能完成 `org` analyzer；不要屏蔽或降级 error          |

更全面的发布要求与 checklist 见仓库现有的 [Grafana datasource plugin 发布与 GitHub Release 要求](./grafana-plugin-release-requirements.md)。
