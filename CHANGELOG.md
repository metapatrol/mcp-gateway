## [1.13.2](https://github.com/metapatrol/mcp-gateway/compare/v1.13.1...v1.13.2) (2026-11-21)


### Bug Fixes

* **deps:** update module github.com/oapi-codegen/oapi-codegen/v2 to v2.9.0 ([0fb87e4](https://github.com/metapatrol/mcp-gateway/commit/0fb87e405a11ebdc8114acf6cd610f34f94d342c))
* **deps:** update module github.com/oapi-codegen/runtime to v1.9.1 ([0b3689a](https://github.com/metapatrol/mcp-gateway/commit/0b3689ab0279544284bbcb5a9b193957138a50c0))
* **deps:** update module sigs.k8s.io/controller-runtime to v0.25.3 ([1f8006b](https://github.com/metapatrol/mcp-gateway/commit/1f8006bb4ac286fd12886f0a7b947bf4ff90cb6a))

## [1.13.1](https://github.com/metapatrol/mcp-gateway/compare/v1.13.0...v1.13.1) (2026-11-15)


### Bug Fixes

* **gateway:** hold a GET open only for a client that asks for the stream ([b73d3eb](https://github.com/metapatrol/mcp-gateway/commit/b73d3eb70ea1d91b810e2a60659e5e69c996af21))

# [1.13.0](https://github.com/metapatrol/mcp-gateway/compare/v1.12.0...v1.13.0) (2026-11-15)


### Bug Fixes

* **deps:** update go dependencies ([e81ebcb](https://github.com/metapatrol/mcp-gateway/commit/e81ebcbe7d266e4ed0cc52e84478a690aa9b5be5))
* **gateway:** refuse requests that do not name MCP 2026-07-28 ([f6b0045](https://github.com/metapatrol/mcp-gateway/commit/f6b00458d53a831eea4dfa2c29f352d358126c18))


### Features

* **gateway:** serve agents of MCP 2025-03-26 to 2025-11-25 without sessions ([f8c5bbe](https://github.com/metapatrol/mcp-gateway/commit/f8c5bbea4b28646543875178f6ac0da625f25855)), closes [#102](https://github.com/metapatrol/mcp-gateway/issues/102) [#103](https://github.com/metapatrol/mcp-gateway/issues/103)

# [1.12.0](https://github.com/metapatrol/mcp-gateway/compare/v1.11.0...v1.12.0) (2026-11-07)


### Bug Fixes

* **deps:** update @metapatrol/brand, ng and style to ^0.9.0 ([a36dba9](https://github.com/metapatrol/mcp-gateway/commit/a36dba9386796d10f10823ec086d2efb3f6574a4)), closes [#65](https://github.com/metapatrol/mcp-gateway/issues/65) [#66](https://github.com/metapatrol/mcp-gateway/issues/66) [#67](https://github.com/metapatrol/mcp-gateway/issues/67)
* **deps:** update go dependencies ([694df86](https://github.com/metapatrol/mcp-gateway/commit/694df86f754fd7dbe9d2ce4df5a3773fadf48611))
* **deps:** update module github.com/prometheus/client_golang to v1.25.0 ([c83a348](https://github.com/metapatrol/mcp-gateway/commit/c83a3487d7ce540950bb65e6cbed31227e4d538a))


### Features

* **crd:** reach SSE-only backends with spec.transport ([87cc0b1](https://github.com/metapatrol/mcp-gateway/commit/87cc0b12a24d6a4d3755316ff078eda0d195d73d)), closes [#69](https://github.com/metapatrol/mcp-gateway/issues/69)
* **gateway:** trace tool calls with OpenTelemetry ([fa63abf](https://github.com/metapatrol/mcp-gateway/commit/fa63abf273d9d37ed787bcf0ac6efdc3b22eb90a)), closes [#68](https://github.com/metapatrol/mcp-gateway/issues/68)

# [1.11.0](https://github.com/metapatrol/mcp-gateway/compare/v1.10.0...v1.11.0) (2026-10-05)


### Bug Fixes

* **console:** redraw the overview's lines when the map changes size ([0a61226](https://github.com/metapatrol/mcp-gateway/commit/0a61226f92743004f619bc08a4b5533279fc3b8f))


### Features

* **console:** bundle what is fine in long columns of the map, on the overview ([fb977a9](https://github.com/metapatrol/mcp-gateway/commit/fb977a97c0faae3eb5114293aa7497ee86b5fc38))
* **console:** focus the overview's map once it has more than 50 boxes ([898aecf](https://github.com/metapatrol/mcp-gateway/commit/898aecf35abfed3ac2263ceabc3ae632b3288eed))
* **console:** load the subjects as the list is scrolled ([566fbd6](https://github.com/metapatrol/mcp-gateway/commit/566fbd69796aa2d535358b9c7958ba966b7b4c59))
* **console:** the map moves to a page of its own, with zoom and pan ([ac527a3](https://github.com/metapatrol/mcp-gateway/commit/ac527a3734b272e6d0409ab4dc731c6c67db7648))
* **inspect:** describe cursor paging, search and sort for the lists ([90fcac6](https://github.com/metapatrol/mcp-gateway/commit/90fcac6391d7bc318b2a05b9bfd4c82a01fe2540))
* **inspect:** page, search and sort the lists on the server ([950f5a3](https://github.com/metapatrol/mcp-gateway/commit/950f5a311c1b0cbefdbd332ccbbd54062c86df64))


### Performance Improvements

* **console:** zoom and pan the map without touching the template ([c6f8450](https://github.com/metapatrol/mcp-gateway/commit/c6f845062be3d91bccf59351ebd9634a04da4e13))

# [1.10.0](https://github.com/metapatrol/mcp-gateway/compare/v1.9.0...v1.10.0) (2026-10-04)


### Bug Fixes

* **deps:** update module sigs.k8s.io/controller-runtime to v0.25.2 ([361d55b](https://github.com/metapatrol/mcp-gateway/commit/361d55bb5de1c6ee3c607798b8777affea2cce26))


### Features

* **console:** the metapatrol look, from @metapatrol/ng ([cbd9d57](https://github.com/metapatrol/mcp-gateway/commit/cbd9d57d85782d48918812eff0c5ccf68f0f8c90))

# [1.9.0](https://github.com/metapatrol/mcp-gateway/compare/v1.8.0...v1.9.0) (2026-09-24)


### Features

* **ui:** estimate what an MCP payload costs in context ([119e5ed](https://github.com/metapatrol/mcp-gateway/commit/119e5ed477b650f1bda9c39521c7c022708142e5))
* **ui:** sort and search every list ([84ad4cc](https://github.com/metapatrol/mcp-gateway/commit/84ad4ccf21d59e8c282fd7861ae34b1bada42af1))

# [1.8.0](https://github.com/metapatrol/mcp-gateway/compare/v1.7.1...v1.8.0) (2026-09-24)


### Features

* **ui:** keep what a page shows in its URL ([b548668](https://github.com/metapatrol/mcp-gateway/commit/b548668042c0c9ac2aa574a4186383027acdd61c))

## [1.7.1](https://github.com/metapatrol/mcp-gateway/compare/v1.7.0...v1.7.1) (2026-09-24)


### Bug Fixes

* **gateway:** serve the endpoint at / with protected resource metadata ([af80b06](https://github.com/metapatrol/mcp-gateway/commit/af80b06a30e0d6d9b43f869fe6cfb5877510e4f6))

# [1.7.0](https://github.com/metapatrol/mcp-gateway/compare/v1.6.0...v1.7.0) (2026-09-24)


### Bug Fixes

* **build:** pin controller-gen to v0.22.0, which generated the CRDs ([a87bb57](https://github.com/metapatrol/mcp-gateway/commit/a87bb5740678bca9d7a25160397cabb0b0e5bc75))
* **deps:** update go dependencies to v0.37.1 ([19d7e28](https://github.com/metapatrol/mcp-gateway/commit/19d7e28acad6e25c75ff8509f36af72c4b6279d5))
* **ui:** drop the failure count from the server list ([4097d7b](https://github.com/metapatrol/mcp-gateway/commit/4097d7b59000013085cedd1d83212618937577bc))
* **ui:** make the subject search filter, and align the subjects page ([fbaa5f0](https://github.com/metapatrol/mcp-gateway/commit/fbaa5f04df8891dbb0412919b4f4f128359bb0fc))
* **ui:** show tool hints only as far as the backend declares them ([831107e](https://github.com/metapatrol/mcp-gateway/commit/831107e0d3e35d6c7a7baf19845bdd665e56ca51))
* **ui:** what a review of every page turned up ([5f21ec9](https://github.com/metapatrol/mcp-gateway/commit/5f21ec995ff49556b13c19aa736b78107029deaf))


### Features

* **dashboard:** an Inspect API row ([6ae7454](https://github.com/metapatrol/mcp-gateway/commit/6ae745452594de807cb798cee975d484fda1c1ee))
* **gateway:** configure instructions of the gateway's own ([66617d5](https://github.com/metapatrol/mcp-gateway/commit/66617d5b1962e89028ae87163d0888a18c3d1005))
* **gateway:** serve a read-only inspect API for the console ([2c6b903](https://github.com/metapatrol/mcp-gateway/commit/2c6b903fff10cd1e6e1978eb3d292af66ff852d5))
* **gateway:** stream changes to the console as they happen ([14ef7d2](https://github.com/metapatrol/mcp-gateway/commit/14ef7d29de84095fda759f75c4d1845673bfed6a))
* **inspect:** serve the API's description publicly ([b9c8a35](https://github.com/metapatrol/mcp-gateway/commit/b9c8a355d7b2601ba740630aa167e68e2e7950c2))
* **metrics:** instrument the inspect API ([933f4f7](https://github.com/metapatrol/mcp-gateway/commit/933f4f79e54ff4b23745b0c46ca29982d4df1399))
* show the full MCP answers, as the gateway gives them ([6f9ff47](https://github.com/metapatrol/mcp-gateway/commit/6f9ff471b2671f0126c7f124f505251f7ffa9d57))
* show the instructions on every detail page ([3dd282e](https://github.com/metapatrol/mcp-gateway/commit/3dd282e1faaf26520ef7083b5c3f9c65420df68d))
* show what each resource hands out as tools/list ([fc8325d](https://github.com/metapatrol/mcp-gateway/commit/fc8325d03447117c9bcf357ff192d85873c71580))
* **ui:** add mcp-gateway-ui, a read-only console ([10adbd5](https://github.com/metapatrol/mcp-gateway/commit/10adbd505629c600660c3972ae1b8e98bce96d46))
* **ui:** count what is not ready in the navigation ([aa2fc3f](https://github.com/metapatrol/mcp-gateway/commit/aa2fc3fb8b832575d23ed1ee7ca9799082d44b3d))
* **ui:** render instructions as Markdown, with a raw view ([472bcba](https://github.com/metapatrol/mcp-gateway/commit/472bcbaf31f95cccf61a3b0b246bc0f3635d869e))
* **ui:** rework the subject pages and the sidebar ([de338ab](https://github.com/metapatrol/mcp-gateway/commit/de338ab7496e9e4e97fcae88777b891da7fa1600))
* **ui:** show the MCP answers in a read-only JSON editor ([3a2f714](https://github.com/metapatrol/mcp-gateway/commit/3a2f714d4d74f57b86e1221ceb151bf107d21e66))
* **ui:** tooltips with what is behind a pill or badge ([acd88fa](https://github.com/metapatrol/mcp-gateway/commit/acd88fa6c484c7c6ab01252feabb89b47f2723bd))

# [1.6.0](https://github.com/metapatrol/mcp-gateway/compare/v1.5.0...v1.6.0) (2026-09-15)


* refactor!: rename the project to mcp-gateway ([0a2fbe1](https://github.com/metapatrol/mcp-gateway/commit/0a2fbe1228bf0007edb0b7e62c1d1757c28e42f7))


### BREAKING CHANGES

* every public name changes, with no aliases for the old ones.

- Binaries and images: mcp-gateway (was mcp-gate-gateway),
  mcp-gateway-controller, mcp-gateway-proxy.
- Charts: mcp-gateway and mcp-gateway-grafana-dashboard. The gateway's
  Deployment, Service, ServiceAccount, ClusterRole, PodDisruptionBudget and
  NetworkPolicy are named after the release (mcp-gateway), no longer with a
  -gateway suffix; its metrics Service is mcp-gateway-metrics.
- API group: mcp-gateway.metapatrol.com/v1alpha1 (was
  mcp.gate.metapatrol.com), category mcp-gateway. The CRDs are new ones:
  resources have to be re-created under the new apiVersion, and the old CRDs
  removed by hand once they are.
- Metrics: mcp_gateway_* (was mcp_gate_*). The dashboard's uid is
  mcp-gateway.
- Labels: mcp-gateway.metapatrol.com/chart-version and
  mcp-gateway.metapatrol.com/metrics.
- Environment: MCP_GATEWAY_* (was MCP_GATE_*); the proxy's gateway URL is
  MCP_GATEWAY_URL.
- Leader election ID: mcp-gateway-controller.metapatrol.com.
- Go module: forgejo.metapatrol.local/platform/mcp-gateway.

# [1.5.0](https://github.com/metapatrol/mcp-gateway/compare/v1.4.1...v1.5.0) (2026-09-15)


### Bug Fixes

* **deps:** update github.com/lestrrat-go/jwx to v4, on Go 1.27 ([4e4c1cc](https://github.com/metapatrol/mcp-gateway/commit/4e4c1cc3f1c798dab9e0a25bc7b0b847eaa6b7a4))


### Features

* **gateway:** relay a backend's progress to the agent that asked for it ([196e187](https://github.com/metapatrol/mcp-gateway/commit/196e1874e053ba36dcbf3b44f0e8c401b4a4464c))
* **toolset:** offer tools under a prefix or aliases ([89647e4](https://github.com/metapatrol/mcp-gateway/commit/89647e437f115c71210846cb3c7e864ca5573402)), closes [#6](https://github.com/metapatrol/mcp-gateway/issues/6)


### Performance Improvements

* **discovery:** send Passthrough calls to MCP 2026-07-28 backends on the shared connection ([54a7389](https://github.com/metapatrol/mcp-gateway/commit/54a7389e87107bb30719f997213bb51d8d93c0a5)), closes [#8](https://github.com/metapatrol/mcp-gateway/issues/8)

## [1.4.1](https://github.com/metapatrol/mcp-gateway/compare/v1.4.0...v1.4.1) (2026-09-15)


### Bug Fixes

* **discovery:** keep connections to backends open under concurrent calls ([d2d025e](https://github.com/metapatrol/mcp-gateway/commit/d2d025e98193ef802d0923834022134658de972a)), closes [#7](https://github.com/metapatrol/mcp-gateway/issues/7)

# [1.4.0](https://github.com/metapatrol/mcp-gateway/compare/v1.3.2...v1.4.0) (2026-09-15)


* feat(gateway)!: speak MCP 2026-07-28 to agents, and nothing older ([28ad4d2](https://github.com/metapatrol/mcp-gateway/commit/28ad4d291d60cd579f565c164ccc8e20d9bd3e99))


### BREAKING CHANGES

* agents must speak MCP 2026-07-28; clients of the session-based
protocol versions are refused. The --stateless and --session-timeout flags and
the chart values gateway.stateless and gateway.sessionTimeout are removed: the
gateway is always stateless.

## [1.3.2](https://github.com/metapatrol/mcp-gateway/compare/v1.3.1...v1.3.2) (2026-09-14)


### Bug Fixes

* **deps:** update go dependencies ([a5a656f](https://github.com/metapatrol/mcp-gateway/commit/a5a656f0e410b9a297241fbf9fb278f223d477d8))

## [1.3.1](https://github.com/metapatrol/mcp-gateway/compare/v1.3.0...v1.3.1) (2026-09-14)


### Performance Improvements

* **auth:** remember verified JWTs until they expire ([fce1b96](https://github.com/metapatrol/mcp-gateway/commit/fce1b962c45f0933e7d82ce3604850bc6e865567)), closes [#5](https://github.com/metapatrol/mcp-gateway/issues/5)

# [1.3.0](https://github.com/metapatrol/mcp-gateway/compare/v1.2.1...v1.3.0) (2026-09-14)


### Features

* **auth:** authenticate to backends that want credentials ([260a225](https://github.com/metapatrol/mcp-gateway/commit/260a2255780df968c850693df7d908884a2d749d))
* **toolset:** select tools by glob pattern, with exclusions ([b5aefaa](https://github.com/metapatrol/mcp-gateway/commit/b5aefaa1e96ad6c9d852cbc69882768834e601f4))

## [1.2.1](https://github.com/metapatrol/mcp-gateway/compare/v1.2.0...v1.2.1) (2026-09-14)


### Bug Fixes

* **auth:** count a JWT under an unknown key ID as a failed signature ([2d9571b](https://github.com/metapatrol/mcp-gateway/commit/2d9571be92b695a2ddfa7f377b49a942613a1c51))

# [1.2.0](https://github.com/metapatrol/mcp-gateway/compare/v1.1.1...v1.2.0) (2026-09-14)


### Features

* **gateway:** pass the backends' instructions on to agents ([3bf613b](https://github.com/metapatrol/mcp-gateway/commit/3bf613b9428ffd1e8c3197b1eff6ecfb4b27e066))

## [1.1.1](https://github.com/metapatrol/mcp-gateway/compare/v1.1.0...v1.1.1) (2026-09-14)


### Bug Fixes

* **discovery:** stop a backend handshake from hanging forever ([51f484a](https://github.com/metapatrol/mcp-gateway/commit/51f484ad862e1ca9efce75a68526d3ad54091106))

# [1.1.0](https://github.com/metapatrol/mcp-gateway/compare/v1.0.0...v1.1.0) (2026-09-14)


### Features

* ship a Grafana dashboard for mcp-gate ([9af9e7c](https://github.com/metapatrol/mcp-gateway/commit/9af9e7c3963f11e1b75139227b4b8b5530311b00))

# 1.0.0 (2026-09-14)


### Features

* initial MCP Gate implementation ([6ef7847](https://github.com/metapatrol/mcp-gateway/commit/6ef78479eca833c6dfc9a328e0cad4a6e84f3815))

# Changelog

All notable changes to this project will be documented in this file.
Generated by [git-cliff](https://git-cliff.org/).
