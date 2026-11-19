<p align="center"><img src="assets/logo/pidief-icon.svg" width="112" alt="pidief logo"></p>

# pidief

[![CI](https://github.com/Milvasoft/pidief/actions/workflows/ci.yml/badge.svg)](https://github.com/Milvasoft/pidief/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Website](https://img.shields.io/badge/website-milvasoft.github.io%2Fpidief-ff5a45.svg)](https://milvasoft.github.io/pidief/)

A self-hosted PDF toolkit with a **CLI and an HTTP API** that do exactly the same things: both are generated
from one operation table (`pidief pages rotate` = `POST /v1/pages/rotate`, same parameters).

- **HTML → PDF** with headless Chromium: A4 or custom paper, margins, landscape, header/footer templates,
  "page X of Y", embedded fonts (Onest included, full Turkish support), CSS print, table headers repeated on
  every page, wait conditions, external resources blocked by default, timeouts
- **Template + JSON → PDF**, and an HTML preview of the same template (Handlebars/Mustache syntax, loops,
  conditions, Turkish money/date/amount-in-words helpers)
- **Images → PDF** for phone scans: JPEG, PNG, WebP, GIF, TIFF, BMP; EXIF orientation, fit to page, DPI cap, compression
- **Merge, split, select/delete/rotate pages, watermark, stamp, compress, PDF/A-2b, encrypt/decrypt, metadata**
- **PDF → image** (PNG/JPEG/WebP, DPI, thumbnails), page info, text extraction

Single static Go binary (no cgo) plus Chromium; Docker image ~256 MiB for linux/amd64 and linux/arm64.
Licensed under [Apache-2.0](LICENSE); every dependency is permissively licensed ([NOTICE](NOTICE)).
Why these choices: [docs/comparison.md](docs/comparison.md).

## Install

```bash
docker run -d -p 3000:3000 --shm-size 256m -e PIDIEF_API_KEYS=change-me ghcr.io/milvasoft/pidief:latest
```

Or build it: `docker build -t pidief .`. A hardened service definition (read-only root, tmpfs, dropped
capabilities, memory limit) is in [docker-compose.yml](docker-compose.yml).

CLI binaries for Linux, macOS and Windows (amd64, arm64) are attached to each
[release](https://github.com/Milvasoft/pidief/releases). HTML and template commands need Chrome or Chromium
on the machine (`PIDIEF_CHROMIUM_PATH` sets the path); all other commands do not.
On macOS use Google Chrome or [chrome-headless-shell](https://developer.chrome.com/blog/chrome-headless-shell)
(`npx @puppeteer/browsers install chrome-headless-shell@stable`): the "Chrome for Testing" bundle often cannot
start its helper processes outside an interactive login session, such as on CI runners.

From source (Go 1.26+):

```bash
go build -tags nodynamic -o bin/pidief ./cmd/pidief
```

## CLI

```bash
pidief html report.html img/logo.png css/report.css --page-numbers --title "Monthly report" -o report.pdf
pidief html contract.html --paper A4 --margin 20mm --pdfa \
  --footer '<div style="font-size:8px;width:100%;text-align:center"><span class="pageNumber"></span> / <span class="totalPages"></span></div>'
pidief template invoice.html data.json --strict --empty "-" -o invoice.pdf
pidief template preview invoice.html data.json -o preview.html
pidief images scan1.jpg scan2.jpg --max-dpi 150 --grayscale -o scans.pdf
pidief merge a.pdf b.pdf -o merged.pdf
pidief split report.pdf --span 5 -o parts/        # or --at 3,7, or --ranges 1-2 --ranges 5-
pidief pages select report.pdf --pages "3,1-2"
pidief pages delete report.pdf --pages "l"
pidief pages rotate scan.pdf --degrees 90 --pages even
pidief watermark contract.pdf --text "DRAFT"
pidief stamp contract.pdf --text "Page %p of %P · APPROVED" --position br --font-size 10 --color "#C00000"
pidief stamp contract.pdf signature.png --position br --scale 0.2
pidief compress scan.pdf --image-quality 60 --max-image-pixels 1600
pidief pdfa report.pdf --title "Statement"
pidief encrypt report.pdf --user-password 1234 --allow-print
pidief decrypt report.pdf --password 1234
pidief metadata report.pdf --title "Receipt" --property DocumentNo=R-2026-08
pidief info report.pdf
pidief text report.pdf
pidief render report.pdf --pages 1 --format webp --max-width 300 -o thumb.webp
pidief openapi > openapi.json
pidief serve
```

`-o -` writes to stdout and a `-` input reads stdin. Results with several files (split, multi-page render)
go to a directory, or to a ZIP with `-o out.zip`. Exit codes: 0 success, 2 invalid input, 1 other errors.
`pidief <command> --help` lists every flag.

## HTTP API

`pidief serve` (the image's default command). OpenAPI 3.1 at `GET /openapi.json`, a reference page at `GET /docs`.

| Endpoint | Operation | Result |
|---|---|---|
| `POST /v1/html` | HTML → PDF | PDF |
| `POST /v1/template` | template + data → PDF | PDF |
| `POST /v1/template/preview` | template + data → HTML | HTML |
| `POST /v1/images` | images → PDF | PDF |
| `POST /v1/merge` | merge | PDF |
| `POST /v1/split` | split | ZIP |
| `POST /v1/pages/select` · `/delete` · `/rotate` | page operations | PDF |
| `POST /v1/watermark` · `/v1/stamp` | watermark, stamp | PDF |
| `POST /v1/compress` | compress | PDF |
| `POST /v1/pdfa` | PDF/A-2b | PDF |
| `POST /v1/encrypt` · `/v1/decrypt` | passwords | PDF |
| `POST /v1/metadata` | document information | PDF |
| `POST /v1/info` · `/v1/text` | info, text | JSON |
| `POST /v1/render` | pages → images | image or ZIP |
| `GET /v1/jobs/{id}` · `/result`, `DELETE /v1/jobs/{id}` | background jobs | JSON / result |
| `GET /health/live` · `/health/ready` · `/metrics` | health, Prometheus | |

Every endpoint accepts three body types:

- `multipart/form-data`: files as parts with a filename (relative paths are kept: `img/logo.png`);
  parameters as separate fields or one `options` JSON field.
- `application/json`: `{"files":[{"name":"a.pdf","content":"<base64>"}], ...parameters}`; `html`, `template`
  and `data` (an object) can be given directly.
- A raw document (`application/pdf`, `image/*`, `text/html`) with parameters in the query string.

```bash
curl -H "X-Api-Key: $KEY" -F files=@report.html -F "files=@img/logo.png;filename=img/logo.png" \
     -F pageNumbers=true http://localhost:3000/v1/html -o report.pdf

curl -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' http://localhost:3000/v1/template \
     -d '{"template":"<h1>{{customer.name}}</h1><p>{{currency total \"EUR\"}}</p>","data":{"customer":{"name":"Example Ltd"},"total":1234.56}}' -o receipt.pdf

curl -H "X-Api-Key: $KEY" -H 'Content-Type: application/pdf' --data-binary @report.pdf \
     'http://localhost:3000/v1/render?pages=1&format=webp&maxWidth=300' -o thumb.webp

# Long work: ?async=true -> 202 {"id":...}; GET /v1/jobs/{id} -> state; GET /v1/jobs/{id}/result
curl -H "X-Api-Key: $KEY" -F files=@big.html 'http://localhost:3000/v1/html?async=true'
```

Errors are RFC 9457 `application/problem+json` (`detail`, `requestId`): 400 invalid input, 401 API key,
413 too large, 429 queue full (`Retry-After`), 503 no free slot in time, 504 timeout.

The service is stateless: inputs and results stay in memory and documents are never written to disk.
Async results are kept in memory for `PIDIEF_JOB_TTL`; with several replicas, async jobs need sticky routing.

### Template language

Mustache placeholders (`{{customer.name}}`, `{{#items}}…{{/items}}`) plus Handlebars blocks and helpers.
Helpers have Turkish and English names.

| | |
|---|---|
| `{{a.b}}` / `{{{html}}}` | HTML-escaped / raw value |
| `{{#each list}}…{{else}}…{{/each}}`, `{{#each list as \|item\|}}` | loop; `@index`, `@number`, `@first`, `@last`, `../parent` |
| `{{#if x}}…{{else if y}}…{{else}}…{{/if}}`, `{{#unless}}`, `{{#with}}`, `{{^list}}` | conditions |
| `{{currency total}}` / `{{para total}}` → ₺1.234,56; `"USD"` → $1.234,56; `"TL"` → 1.234,56 TL | money (tr-TR grouping) |
| `{{number x 2}}` / `{{sayi x 2}}` → 1.234,57; `{{percent 18}}` / `{{yuzde 18}}` → %18 | numbers |
| `{{date d}}` / `{{tarih d}}` → 18.11.2026; `{{date d "d MMMM yyyy dddd"}}` → 18 Kasım 2026 Çarşamba; `{{today}}` | dates (.NET format letters, Turkish month/day names) |
| `{{inwords 1234.56}}` / `{{yaziyla 1234.56}}` → bin iki yüz otuz dört Türk lirası elli altı kuruş | amount in words (Turkish) |
| `{{upper "istanbul"}}` → İSTANBUL, `lower`, `title` | Turkish-aware casing |
| `{{sum items "amount"}}`, `add/sub/mul/div/round`, `eq/ne/gt/lt/and/or/not`, `default`, `join`, `length` | math (exact decimals) and logic |

`--strict` fails on a missing value; `--empty "-"` is printed for empty values. Dates use the `timezone`
parameter (default `Europe/Istanbul`).

## .NET client

`clients/dotnet/src/Pidief.Client` (net8.0 and net10.0), published as the `Pidief.Client` NuGet package.

```csharp
builder.Services.AddPidiefClient(o =>
{
    o.BaseAddress = new Uri(builder.Configuration["Pidief:Url"]!);
    o.ApiKey = builder.Configuration["Pidief:ApiKey"];
    o.Timeout = TimeSpan.FromSeconds(60);   // per attempt
    o.MaxRetries = 3;                        // 429/502/503/504, network errors, attempt timeouts; honours Retry-After
});

public sealed class ReportPdf(IPidiefClient pidief)
{
    public async Task<byte[]> CreateAsync(string html, byte[] logo, CancellationToken ct)
    {
        var pdf = await pidief.HtmlToPdfAsync(html,
            new HtmlOptions { PageNumbers = true, Title = "Monthly report", PdfA = true },
            [new PidiefFile("img/logo.png", logo)], ct);
        var thumbnail = await pidief.RenderAsync(new PidiefFile("report.pdf", pdf),
            new RenderOptions { Pages = "1", Format = "webp", MaxWidth = 300 }, ct);
        return pdf;
    }
}
```

Errors throw `PidiefException` (`StatusCode`, `RequestId`, `IsInvalidRequest`). Long work: `StartJobAsync` + `WaitForJobAsync`.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PIDIEF_ADDR` | `:3000` | listen address |
| `PIDIEF_API_KEYS` | (empty) | comma-separated keys for `X-Api-Key`; empty disables authentication (development only) |
| `PIDIEF_MAX_REQUEST_MB` | `50` | request body limit |
| `PIDIEF_CONCURRENCY` | `min(4, CPUs)` | operations running at once |
| `PIDIEF_QUEUE_SIZE` | `100` | requests that may wait for a slot; more get 429 |
| `PIDIEF_QUEUE_TIMEOUT` | `30s` | how long a request waits for a slot before 503 |
| `PIDIEF_MAX_TIMEOUT` | `120s` | upper bound of the `timeout` parameter |
| `PIDIEF_ALLOW_EXTERNAL` | `false` | allow the `allowExternal` parameter (HTML loading internet resources) |
| `PIDIEF_JOB_TTL` | `15m` | how long finished job results are kept |
| `PIDIEF_MAX_JOBS` | `200` | jobs kept in memory |
| `PIDIEF_METRICS_PUBLIC` | `true` | serve `/metrics` without a key |
| `PIDIEF_CHROMIUM_PATH` | auto | Chromium path (`/usr/bin/chromium-headless-shell` in the image) |
| `PIDIEF_CHROMIUM_NO_SANDBOX` | `false` | `true` in the image (containers lack user namespaces) |
| `PIDIEF_CHROMIUM_RESTART_AFTER` | `200` | restart Chromium after this many conversions |
| `PIDIEF_RASTER_WORKERS` | `2` | PDFium WebAssembly instances |
| `PIDIEF_SHUTDOWN_TIMEOUT` | `30s` | graceful shutdown wait |
| `PIDIEF_LOG_LEVEL` / `PIDIEF_LOG_FORMAT` | `info` / `json` | structured logs (slog) |

Metrics: `pidief_operations_total{op,status}`, `pidief_operation_duration_seconds`, `pidief_input_bytes`,
`pidief_operations_running`, `pidief_operations_waiting`, plus Go and process metrics.

## Resource usage

`scripts/measure.sh pidief:dev 8 40` on Docker Desktop (amd64), `PIDIEF_CONCURRENCY=4`, a 300-row table
rendered to 10 pages with page numbers:

| Measure | Value |
|---|---|
| Image size (amd64 / arm64) | 256 MiB / 258 MiB |
| Idle memory (Chromium warm) | ~71 MiB |
| Peak memory, 8 concurrent requests | ~191-194 MiB |
| 40 requests, 8 concurrent | 40/40 ok in 8.5 s |
| Latency (10 pages) | p50 0.62-0.75 s, p95 1.2-1.8 s |

Numbers depend on the CPU. In production, a 1 GB memory limit and `--shm-size 256m` are recommended.

## Tests

```bash
go test ./...                                                       # unit + Chromium integration (skipped without Chromium)
PIDIEF_REQUIRE_BROWSER=1 PIDIEF_VERAPDF=1 go test ./internal/ops/   # require Chromium; validate PDF/A with veraPDF (Docker)
dotnet test clients/dotnet/Pidief.Client.slnx
PIDIEF_URL=http://localhost:3000/ PIDIEF_API_KEY=... dotnet test clients/dotnet/Pidief.Client.slnx   # live end-to-end
```

The integration tests generate real PDFs and check page counts, extracted text (Turkish characters),
embedded fonts (Onest included), repeated table headers, page number footers, blocked external resources,
timeouts, the PDF/A claim and the veraPDF verdict.

## Out of scope

- **Office → PDF:** the only clean option, LibreOffice (MPL-2.0), would roughly quadruple the image. It may
  come later as a separate `pidief-office` image.
- **HEIC:** the decoder (libde265) is LGPL and HEVC has patent risk. Send JPEG; HEIC input returns a clear 400.
- **PDF/A-1, PDF/A-3, PDF/UA:** only PDF/A-2b. PDFs with non-embedded fonts are rejected for PDF/A; colours are not converted.
- **Digital signatures, form filling, OCR.**

## Türkçe özet

pidief, HTML/şablon/görsellerden PDF üreten ve PDF'leri düzenleyen (birleştir, böl, döndür, filigran, damga,
sıkıştır, PDF/A-2b, şifre, metadata, görsele çevir, metin çıkar) açık kaynak bir araçtır; aynı işlemler CLI'da
ve HTTP API'de birebir bulunur. Türkçe karakterler ve gömülü Onest fontu varsayılan olarak çalışır; varsayılan
sayfa numarası metni "Sayfa X / Y", şablon yardımcıları tr-TR biçimlidir (`{{para}}`, `{{tarih}}`, `{{yaziyla}}`).
Kurulum: `docker run -p 3000:3000 -e PIDIEF_API_KEYS=... ghcr.io/milvasoft/pidief`. .NET için `Pidief.Client` paketi.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
Changes are listed in [CHANGELOG.md](CHANGELOG.md); versions follow [SemVer](https://semver.org).

## License

Copyright 2026 Milvasoft. Licensed under the [Apache License 2.0](LICENSE). Third-party notices: [NOTICE](NOTICE).
