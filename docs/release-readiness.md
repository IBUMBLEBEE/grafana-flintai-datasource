# Release readiness audit

Audit date: 2026-09-23

Status: **NOT READY FOR A PUBLIC GITHUB/GRAFANA RELEASE**

The code, tests, build matrix, package layout, and release automation are substantially ready. The repository-local metadata issues have been corrected. A private staging tag has been pushed to exercise the release automation, but public release remains blocked by external organization, classification, and distribution prerequisites.

## Blocking items

1. **The Grafana Cloud organization prefix is not registered.**
   - Validator reports `unregistered Grafana Cloud account: ibumblebee` and `organization not found`.
   - Create/choose the owning Grafana Cloud organization first. Its slug must agree with the final plugin ID prefix.
2. **The GitHub repository is not yet a public release source.**
   - The repository has been renamed to the confirmed canonical name `IBUMBLEBEE/grafana-flintai-datasource`; `main`, existing tags, project metadata, and issue links consistently use that spelling.
   - The repository remains private and is not anonymously accessible. Make it public before publishing a GitHub Release or submitting its source and artifact URLs to Grafana.
3. **The plugin classification needs confirmation from Grafana.**
   - The plugin depends on commercial OpenAI and DeepSeek services. Under Grafana's policy this likely falls under Commercial rather than Community classification, but Grafana makes the final determination.
   - Confirm the classification and any Commercial Plugin Subscription requirement with Grafana before tagging the release.
4. **The public release must be revalidated after the repository becomes public.**
   - The reviewed provider/model-selection, security, and release-preparation changes are committed and tagged from a clean revision.
   - After the public repository and final public tag exist, rerun Plugin Validator using the exact public tag source URL and release asset URL. A private/local-only validation cannot prove anonymous source/archive correspondence or public provenance.

## Completed preparation

- Added CI for type checking, linting, frontend tests/build, Go linting, race tests, six-platform backend builds, package metadata validation, and a Grafana E2E version matrix.
- Changed the plugin ID to the schema-compliant `ibumblebee-flintai-datasource` and synchronized runtime registration, frontend constants, provisioning, local runtime overrides, tests, and documentation.
- Set the initial release version consistently to `0.1.0`.
- Added the official Grafana API compatibility workflow.
- Added a two-stage release helper (`scripts/release-pre.sh`) with stable-SemVer validation, idempotent preparation, clean-tree enforcement, and local annotated-tag creation. It never commits or pushes.
- Added a tag-triggered gated release workflow. Its read-only verification job checks version/Tag/Changelog consistency and reruns frontend, backend, vulnerability, and security checks before the write-capable official `build-plugin` job uses Node 22, Go 1.26.6, and `buildAll` to create a draft with the release artifacts. The workflow then publishes that verified draft as the latest GitHub Release. Provenance attestation is enabled automatically for public repositories and disabled while this repository is private because private attestations require GitHub Enterprise Cloud.
- Replaced the Linux-only Mage setup with Grafana SDK Mage targets for Linux amd64/arm/arm64, Windows amd64, and macOS amd64/arm64.
- Upgraded Grafana frontend dependencies to 13.2.2 and React 19.3.0.
- Upgraded `grafana-plugin-sdk-go` to 0.296.5, Go to 1.26.6, and vulnerable transitive Go dependencies to current fixed releases.
- Added a real configuration screenshot and catalog-facing metadata.
- Added canonical project, source, and issue links for `IBUMBLEBEE/grafana-flintai-datasource`.
- Replaced the Apache-2.0 appendix placeholders with the project copyright owner/year; the Validator license analyzer now passes without warning.
- Removed README relative links that break on the Grafana catalog page.
- Set the tested minimum Grafana version to 13.1.0. Grafana 12.3 does not expose the Combobox open-state callback required by model discovery; Grafana 13.1 and 13.2.2 do.
- Added and stabilized datasource configuration E2E coverage, including OpenAI, DeepSeek, model discovery, custom models, transient credentials, authentication failure, unavailable model catalogs, connection testing, and chat.
- Restricted transient provider configuration routes to organization administrators, added DNS-pinned public-address enforcement, and bounded provider traffic per user and datasource instance.
- Pinned every GitHub Action to an immutable commit SHA and repaired the local backend build script.

## Verification results

| Check                                    | Result                                                                                                                                           |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| TypeScript typecheck                     | Pass                                                                                                                                             |
| ESLint                                   | Pass; tooling prints one package deprecation notice                                                                                              |
| Frontend unit tests                      | 31/31 pass; open-handle detection is clean                                                                                                       |
| Go race tests                            | Pass                                                                                                                                             |
| Go coverage                              | 76.0%                                                                                                                                            |
| Backend multi-platform build             | Pass; six binaries generated                                                                                                                     |
| GitHub Actions syntax (`actionlint`)     | Pass                                                                                                                                             |
| Grafana 13.1 E2E                         | 6/6 pass                                                                                                                                         |
| Grafana 13.2.2 E2E                       | 6/6 pass after the ID/version correction; three consecutive pre-correction runs also passed                                                      |
| Package structure                        | Pass; one ID-named top directory                                                                                                                 |
| Backend ZIP permissions                  | Pass; all binaries are `0755`                                                                                                                    |
| Go vulnerability analyzer after upgrades | Pass with no finding                                                                                                                             |
| Plugin Validator                         | Metadata, ID, version, package, license, binary, security, and vulnerability checks pass; only the external organization lookup remains an error |

The `0.1.0` local audit package was approximately 56 MB and contained README, CHANGELOG, LICENSE, `plugin.json`, frontend assets, screenshot, source map, Go build manifest, and all six backend binaries. It is an audit artifact, not the final release asset.

- Candidate: `artifacts/ibumblebee-flintai-datasource-0.1.0.zip`
- SHA1: `b5e4225a00045fe71e899fa8d75529cbf27a92c8`

## Remaining warnings and accepted risks

- The initial review package may be unsigned. Grafana documents this as expected for a new public plugin; after approval, configure `GRAFANA_ACCESS_POLICY_TOKEN` and require a valid `MANIFEST.txt` for subsequent public releases.
- `npm audit` reports four moderate advisories through `@grafana/ui` → `react-router-dom-v5-compat` → `react-router`. The proposed automated fix downgrades Grafana UI to 11.2.10 and is a breaking/incompatible change, so this remains an upstream dependency risk to monitor.
- The legacy `@stylistic/eslint-plugin-ts` package prints a deprecation notice. It does not fail linting; migrate to the unified package in a separate tooling update.
- A sponsorship link is optional and was not added.

## Required release sequence

1. Register or select the owning Grafana Cloud organization with slug `ibumblebee`.
2. Confirm Community/Commercial classification and subscription requirements with Grafana.
3. Make `IBUMBLEBEE/grafana-flintai-datasource` public and verify anonymous access to the reviewed default branch and release assets.
4. Review the full working tree, run all checks, commit, and ensure CI is green on a clean revision.
5. Run `./scripts/release-pre.sh X.Y.Z`, review and commit any generated release-file changes, then run `./scripts/release-pre.sh X.Y.Z --tag` from the clean release commit.
6. Rebuild and run Plugin Validator against the final archive and the exact public source tag. Resolve every error and explicitly accept or fix each warning.
7. Review the local annotated tag, then push it; the version must match `package.json`, `package-lock.json`, and `CHANGELOG.md` exactly.
8. Let the release workflow pass its verification job, create the artifact draft, and publish it; then verify the ZIP, `.sha1`, attestation, release notes, and anonymous download URLs.
9. Submit the public release asset URL, tag-fixed source URL, SHA1, architecture choice, and testing guidance from the owning Grafana Cloud organization.
10. After Grafana grants a public signing level, add the access-policy secret and require signed artifacts for future releases.

See [Grafana plugin release requirements](./grafana-plugin-release-requirements.md) for the policy research, official-source links, and detailed submission checklist.
