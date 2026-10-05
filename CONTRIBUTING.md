# Contributing

Thanks for taking the time.

## How this repository works

mcp-gateway is developed in a private Forgejo instance and push-mirrored here.
Issues and pull requests opened on GitHub are carried into Forgejo by
[forgejo-github-bridge](https://github.com/metapatrol/forgejo-github-bridge), where the maintainers work, and everything
they say comes back here. From your side it is an ordinary GitHub project; a
bot account posts the maintainers' replies, naming who wrote them.

## Issues

Open an issue for bugs, questions and ideas. Please include the version, what
you expected, what happened, and the relevant output — with tokens and keys
removed.

## Pull requests

- Target **`develop`**. Pull requests against other branches are not picked up.
- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:` and
  `fix:` make a release, `chore:`, `ci:`, `docs:` and `build:` do not.
  Versions and the changelog are derived from them.
- Run the checks before pushing: `make test lint`, `make integration-test`
  (envtest), `make ui-test ui-lint` for the console, and `make generate
  generate-crds generate-api helm-docs` when you change the API, the inspect
  spec or a chart — CI fails when generated files are out of date.
- Explain the why in the commit body when the diff does not.
- [Development](docs/development.md) describes the layout, the tests, running
  everything locally, and where the console's packages come from outside the
  maintainers' network.
- If you change CI configuration (`.forgejo/`, `.github/`), say why in the
  description. Such pull requests are read with particular care before any
  workflow of theirs runs.

### What happens after you open one

1. Your branch is imported into the maintainers' Forgejo. CI runs there once a
   maintainer has read your changes and approved the run.
2. Review comments appear on your pull request, posted by the bot and
   attributed to the maintainer.
3. When it is merged, the bot says so; your authorship is kept.

The brand assets in `.metapatrol/` are generated: `npx @metapatrol/brand-kit init`
rewrites them from `metapatrol.json`. Edit `metapatrol.json`, not the files.
