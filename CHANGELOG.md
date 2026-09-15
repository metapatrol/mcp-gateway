# [1.5.0](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.4.1...v1.5.0) (2026-09-15)


### Bug Fixes

* **deps:** update github.com/lestrrat-go/jwx to v4, on Go 1.27 ([4e4c1cc](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/4e4c1cc3f1c798dab9e0a25bc7b0b847eaa6b7a4))


### Features

* **gateway:** relay a backend's progress to the agent that asked for it ([196e187](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/196e1874e053ba36dcbf3b44f0e8c401b4a4464c))
* **toolset:** offer tools under a prefix or aliases ([89647e4](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/89647e437f115c71210846cb3c7e864ca5573402)), closes [#6](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/issues/6)


### Performance Improvements

* **discovery:** send Passthrough calls to MCP 2026-07-28 backends on the shared connection ([54a7389](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/54a7389e87107bb30719f997213bb51d8d93c0a5)), closes [#8](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/issues/8)

## [1.4.1](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.4.0...v1.4.1) (2026-09-15)


### Bug Fixes

* **discovery:** keep connections to backends open under concurrent calls ([d2d025e](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/d2d025e98193ef802d0923834022134658de972a)), closes [#7](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/issues/7)

# [1.4.0](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.3.2...v1.4.0) (2026-09-15)


* feat(gateway)!: speak MCP 2026-07-28 to agents, and nothing older ([28ad4d2](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/28ad4d291d60cd579f565c164ccc8e20d9bd3e99))


### BREAKING CHANGES

* agents must speak MCP 2026-07-28; clients of the session-based
protocol versions are refused. The --stateless and --session-timeout flags and
the chart values gateway.stateless and gateway.sessionTimeout are removed: the
gateway is always stateless.

## [1.3.2](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.3.1...v1.3.2) (2026-09-14)


### Bug Fixes

* **deps:** update go dependencies ([a5a656f](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/a5a656f0e410b9a297241fbf9fb278f223d477d8))

## [1.3.1](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.3.0...v1.3.1) (2026-09-14)


### Performance Improvements

* **auth:** remember verified JWTs until they expire ([fce1b96](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/fce1b962c45f0933e7d82ce3604850bc6e865567)), closes [#5](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/issues/5)

# [1.3.0](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.2.1...v1.3.0) (2026-09-14)


### Features

* **auth:** authenticate to backends that want credentials ([260a225](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/260a2255780df968c850693df7d908884a2d749d))
* **toolset:** select tools by glob pattern, with exclusions ([b5aefaa](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/b5aefaa1e96ad6c9d852cbc69882768834e601f4))

## [1.2.1](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.2.0...v1.2.1) (2026-09-14)


### Bug Fixes

* **auth:** count a JWT under an unknown key ID as a failed signature ([2d9571b](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/2d9571be92b695a2ddfa7f377b49a942613a1c51))

# [1.2.0](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.1.1...v1.2.0) (2026-09-14)


### Features

* **gateway:** pass the backends' instructions on to agents ([3bf613b](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/3bf613b9428ffd1e8c3197b1eff6ecfb4b27e066))

## [1.1.1](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.1.0...v1.1.1) (2026-09-14)


### Bug Fixes

* **discovery:** stop a backend handshake from hanging forever ([51f484a](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/51f484ad862e1ca9efce75a68526d3ad54091106))

# [1.1.0](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/compare/v1.0.0...v1.1.0) (2026-09-14)


### Features

* ship a Grafana dashboard for mcp-gate ([9af9e7c](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/9af9e7c3963f11e1b75139227b4b8b5530311b00))

# 1.0.0 (2026-09-14)


### Features

* initial MCP Gate implementation ([6ef7847](http://forgejo-http.forgejo.svc.cluster.local:3000/platform/mcp-gate/commit/6ef78479eca833c6dfc9a328e0cad4a6e84f3815))

# Changelog

All notable changes to this project will be documented in this file.
Generated by [git-cliff](https://git-cliff.org/).
