# CI

Three workflows, split by what triggers them. The shape is forge-operator's, so
that what is learned about this Forgejo instance in one repository applies to the
other.

| workflow | runs on | does |
|---|---|---|
| `ci.yml` | pull requests, and pushes to develop | secret scan, lint, unit tests with the race detector, integration tests against a real API server, generated-file drift, chart lint, render and docs |
| `main.yml` | pushes to main | the checks again, then three images from the commit, then semantic-release |
| `release.yml` | a `v*` tag, and `workflow_dispatch` | validate the tag, test, three tagged images, the Forgejo release, binaries, both charts, docs rebuild |

## Ordering

A cheap check gates an expensive one:

    ci.yml       secret-scan ─┐
                 lint ────────┴─> test, integration, generated, helm

    main.yml     config, secret-scan, lint ─> verify ─┬─> image (x3)
                                                     └─> semantic-release

    release.yml  validate ─> test ─> image (x3) ─> notes ─┬─> binaries ─┬─> docs
                                                          └─> chart ────┘

In `release.yml` nothing is published until all three images are pushed: `notes`
creates the Forgejo release, and a release whose images do not exist is worse
than none. `binaries` and `chart` follow `notes` because both attach assets.

`chart` publishes both charts at the release version: `mcp-gateway` and
`mcp-gateway-grafana-dashboard`.

## Images

One Dockerfile, three images, selected with `--build-arg BINARY=`:

| image | binary |
|---|---|
| `images/platform/mcp-gateway-controller` | control plane |
| `images/platform/mcp-gateway` | data plane |
| `images/platform/mcp-gateway-proxy` | the sidecar — kept separate so agent pods do not carry the others |
| `images/platform/mcp-gateway-ui` | the console, with its web UI compiled in |

Every image is tagged with the version (releases), `latest`, the full commit SHA
and its twelve-character prefix. The prefix is what Flux writes into a chart
version's build metadata when a HelmRelease reads the chart from Git, and the
chart derives the image tag from it — so a cluster tracking a branch runs the
commit it tracks.

## How a release actually happens

Not through the tag. Forgejo raises **no push event for a tag created with an
access token**, so `on: push: tags` never fires for the tag semantic-release
makes. `.releaserc.json` runs `.forgejo/trigger-release.sh` from
semantic-release's `successCmd`, which dispatches `release.yml` directly.
`successCmd` runs only when a version was released.

`on: push: tags` stays for tags pushed by hand, which do raise the event.

## Before the first run

**The runner label.** Every job says `runs-on: docker` — a label, not an image.
If the runners are registered under another label, the workflows sit queued.

**Fully qualified base images.** podman does not guess registries, so the
Dockerfile's `FROM` names zot: `zot.metapatrol.local/library/golang:1.27-alpine`.
The image jobs build with `--tls-verify=false` because zot uses the instance's
private CA.

**The race detector** needs cgo and a C compiler; the test jobs install `gcc` when
the runner image lacks it.

**Integration tests** download kube-apiserver and etcd through `setup-envtest`, at
the versions the Makefile pins. A runner that cannot reach the download fails the
`integration` job and nothing else.

**The console's packages.** `@metapatrol/*` come from the forge's npm registry
(`web/ui/.npmrc`). The jobs read them with `AUTOMATION_TOKEN`, passed as
`NPM_TOKEN` because that is the name `web/ui/.npmrc` expects. The registry is
served with a certificate from the platform's private CA, which the job
containers do not trust: `PLATFORM_CA` carries it. A CA certificate is public,
so a variable — ideally one on the organisation, shared by every repository —
is enough. The image build receives both as build secrets (`npm_token`,
`platform_ca`), never as a build argument, so they stay out of the image; it
resolves the forge in the job and hands the address in, because the build does
not inherit the job container's name resolution. The GitHub mirror cannot
reach the forge: it builds `mcp-gateway-ui` with the same versions from GitHub
Packages, where the brand's own GitHub mirror publishes them
(`.github/workflows/images.yml`).

**Secrets and variables.**

| name | kind | value | needed by |
|---|---|---|---|
| `ZOT_URL` | variable | the registry, e.g. `https://zot.metapatrol.local` | the image jobs, `release.chart` |
| `ZOT_USERNAME` | variable | registry login | the same |
| `ZOT_TOKEN` | secret | registry password or token | the same |
| `PLATFORM_CA` | variable (or secret) | the platform CA certificate, PEM | every job that installs the console's packages: the job containers do not carry the platform CA, and npm fails with `UNABLE_TO_VERIFY_LEAF_SIGNATURE` without it |
| `AUTOMATION_TOKEN` | secret | Forgejo access token | every job that installs the console's packages (as `NPM_TOKEN`: `ci.ui`, `main.verify`, the image jobs, `release.binaries`), `main.semantic-release`, `release.notes`, `release.binaries`, `release.chart`, `release.docs` |

Without the registry values `main.yml` skips the image jobs rather than failing;
`release.yml` fails, because asking for a release that cannot be published should
not pass quietly.

`AUTOMATION_TOKEN` needs repository write (releases, assets, the changelog commit
and tag), `write:package` for the generic package registry (which includes
reading the `@metapatrol` npm packages), and the right to dispatch workflows.

**Nothing may be called `FORGEJO_TOKEN`.** The runner injects a variable of that
name holding its own ephemeral token, and it wins over a job env of the same name:
requests then go out as the bot, writes to this repository still succeed, and the
package upload and cross-repository dispatch fail in ways that read as a scope
problem.

## Layout

    .forgejo/
      README.md             this file
      trigger-release.sh    starts release.yml for the tag semantic-release made
      workflows/            ci.yml, main.yml, release.yml

Locally:

    make secret-scan           # the working tree, what ci.yml runs
    make secret-scan-history   # the full history, for an audit
    make lint test integration-test
