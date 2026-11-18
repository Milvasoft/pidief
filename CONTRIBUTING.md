# Contributing to pidief

Thanks for helping. Bug reports, fixes, docs and new operations are welcome.

## Before you start

- For anything larger than a small fix, open an issue first so we can agree on the approach.
- Security problems go through [SECURITY.md](SECURITY.md), not public issues.
- By contributing you agree that your work is licensed under the [Apache License 2.0](LICENSE).

## Development setup

- Go 1.26 or newer
- Chrome or Chromium for the HTML/template tests (`PIDIEF_CHROMIUM_PATH` if it is not found automatically)
- .NET SDK 10 for the client in `clients/dotnet`
- Docker (optional) for the image and the veraPDF PDF/A check

```bash
go build -tags nodynamic -o bin/pidief ./cmd/pidief
go vet ./...
go test ./...
PIDIEF_REQUIRE_BROWSER=1 go test ./internal/ops/   # fail instead of skip when Chromium is missing
dotnet test clients/dotnet/Pidief.Client.slnx
```

CI also runs `golangci-lint` (configuration in `.golangci.yml`) and CodeQL.

## How the code is organised

| Path | What |
|---|---|
| `internal/ops` | the operation registry: every CLI command and API endpoint is one entry here |
| `internal/cli`, `internal/server` | the generated CLI and HTTP API |
| `internal/chromium` | HTML to PDF |
| `internal/pdfops` | pdfcpu-based operations, PDF/A |
| `internal/raster` | PDFium WebAssembly: render and text |
| `internal/imgpdf` | images to PDF |
| `internal/tmpl` | template engine |
| `clients/dotnet` | .NET client |

To add an operation, add a params struct and an entry in `internal/ops/registry.go`. The CLI command, the
endpoint and the OpenAPI schema follow from it. Then add a typed method to the .NET client.

## Pull requests

- Keep each PR focused and include tests for behaviour changes.
- Run `gofmt`, `go vet`, the tests and the linter before pushing.
- Write commit messages in English, in the imperative ("Add page labels to info").
- Add a line under "Unreleased" in [CHANGELOG.md](CHANGELOG.md) for user-visible changes.
- New dependencies must use a license compatible with Apache-2.0 (MIT, BSD, Apache, MPL, ISC). GPL, LGPL
  and AGPL code cannot be linked in. List new dependencies in [NOTICE](NOTICE).

## Releases

Versions follow [Semantic Versioning](https://semver.org). A maintainer tags `vX.Y.Z` on `master`; the release
workflow builds the CLI binaries, the multi-arch image `ghcr.io/milvasoft/pidief` and the `Pidief.Client` package.
