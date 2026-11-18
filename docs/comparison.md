# PDF tools compared, and the design of pidief

Goal: one self-hosted tool, as a CLI and an HTTP API, that covers what business applications need from
PDFs (contracts and documents from HTML templates, reports with long tables, statements, receipts,
scans from phones, previews in a file manager, merge/split/rotate, watermarks, passwords, PDF/A),
with no licensing traps.

## 1. The tools

| Tool | Strengths | Gaps | License / trap | Memory, image |
|---|---|---|---|---|
| **QuestPDF** (.NET) | Fluent C# layout API, table splitting with repeated headers, headers/footers, "page X of Y", font embedding, PDF/A | No HTML/CSS input (cannot print HTML from a template editor), no rasterizing, little manipulation | **Community license is free only below USD 1M annual revenue**; above that it is paid (Professional/Enterprise) | In process, low (native Skia) |
| **Gotenberg** (Go) | Chromium and LibreOffice behind a REST API; multipart API, header/footer templates, wait conditions, webhooks, PDF/A (via LibreOffice/qpdf), merge | Large image (~1.5-2 GB because of LibreOffice); no PDF to image; little watermark/password support; no CLI | MIT (good). LibreOffice is MPL, Chromium BSD | 1-2 GB RAM recommended |
| **PDFsharp / MigraDoc** (.NET) | Low-level PDF writing/reading, merge, document model in MigraDoc | No HTML; partial table splitting; no rasterizing; manual font resolvers | **Moved to AGPL in 6.5** (MIT before). A trap for closed-source SaaS | Low |
| **wkhtmltopdf** | Simple, single binary | **Archived in 2023, unmaintained**; old QtWebKit: no flex/grid, known vulnerabilities; buggy repeated table headers | LGPL-3 | Medium |
| **WeasyPrint** (Python) | Among the best CSS Paged Media engines: `@page`, `counter(page)`/`counter(pages)`, header/footer in margin boxes, PDF/A-1b/2b/3b/4, PDF/UA, font embedding and subsetting | No JavaScript (no chart libraries, no client-side templates); partial flex/grid; Python + Pango dependencies | BSD-3 (good) | ~100-300 MB, image ~250 MB |
| **Playwright/Puppeteer (Chromium PDF)** | Highest HTML/CSS/JS fidelity (flex, grid, web fonts, SVG, canvas), `thead` repeated on every page, header/footer templates (`pageNumber`, `totalPages`), `@page` size and margins, subset font embedding | No PDF/A, no passwords, no manipulation; header/footer templates do not see the page's CSS or web fonts (need system fonts and inline styles); Node dependency | Apache-2.0 (Playwright/Puppeteer), Chromium BSD | Chromium process 100-300 MB |
| **pdfcpu** (Go) | Pure Go manipulation: merge, split, select, delete, rotate, watermark/stamp (text, image, PDF), AES-256 encryption, optimize, metadata, image import, validation | No rasterizing, no HTML, no PDF/A conversion, no image recompression; text watermarks need a user font for non-Latin-1 text (ş, ğ, ı, İ) | **Apache-2.0** | Very low, single binary |
| **qpdf** (C++) | Structural transforms, linearization, encryption, repair; very robust | No rasterizing, no HTML, no watermarks | Apache-2.0 | Low, native dependency |
| **Ghostscript** | PDF/A conversion, image downsampling, rasterizing | Heavy, long history of vulnerabilities | **AGPL-3 or a commercial Artifex license: trap** | Medium |
| **pdf.js** (JS) | Viewing/rasterizing in the browser, text layer | Needs Node canvas or a browser on the server | Apache-2.0 | In the browser |
| **Stirling-PDF** | Web UI and API for every operation (merge, OCR, convert...) | Java + LibreOffice + Tesseract + Ghostscript...: very large (~1-2 GB), an application rather than a component | Core MIT, but **some features are "Pro"/commercial**, and it calls AGPL Ghostscript: careful | 1+ GB |
| **iText** (Java/.NET) | Very mature: PDF/A, PDF/UA, signatures, pdfHTML add-on | — | **AGPL-3 or an expensive commercial license: the classic trap** | Medium |
| **PDFium** (Google, Chromium's PDF engine) | Rasterizing, text extraction, forms, page info; Chrome quality | No layout/creation | **BSD-3 / Apache-2.0** | Low; a WebAssembly build runs from pure Go (wazero) |
| **MuPDF / Poppler** | Fast rasterizing | — | MuPDF **AGPL**, Poppler **GPL**: traps | — |

### Feature details

| Topic | Best examples | Note |
|---|---|---|
| Non-Latin text (Turkish) and font embedding | Chromium, WeasyPrint, QuestPDF: TrueType subsets with ToUnicode | The standard 14 fonts (Helvetica...) use WinAnsi and cannot encode ş, ğ, ı, İ, so pdfcpu text watermarks need an embedded TTF. Chromium embeds *variable* fonts as Type3 glyph procedures, so pidief ships static font instances |
| Header/footer and "page X of Y" | WeasyPrint (`@page` margin boxes), Chromium (`headerTemplate` + `.pageNumber/.totalPages`), QuestPDF | Chromium renders the templates as separate documents that cannot load web fonts, so the Docker image installs Onest as a system font |
| Table splitting | Chromium (`thead`/`tfoot` repeat, `break-inside: avoid` on rows), QuestPDF | Buggy in wkhtmltopdf |
| PDF/A | WeasyPrint (2b/3b), Ghostscript (AGPL), iText (AGPL), QuestPDF | Chromium does not produce it: add XMP metadata, an output intent and annotation flags afterwards |
| PDF to image | PDFium, MuPDF (AGPL), Poppler (GPL), Ghostscript (AGPL) | The permissively licensed one is PDFium |
| Office to PDF | LibreOffice (MPL-2.0) | Clean license, but ~500-700 MB and slow: not lightweight |

## 2. What pidief takes from each

- **Gotenberg:** the multipart API (a main `index.html` plus fonts/images/CSS uploaded next to it and served
  from one virtual origin), `waitForSelector`/`waitForExpression`/`waitDelay`, header/footer templates,
  per-request timeouts, health endpoints, Prometheus metrics.
  **Gaps closed:** a small image (no LibreOffice), a CLI, PDF to image, watermarks, passwords, PDF/A-2b,
  blocking external resources with one switch (SSRF protection), reusing one Chromium process.
- **Playwright/Chromium:** the print engine itself (highest HTML/CSS/JS fidelity).
- **WeasyPrint/QuestPDF:** PDF/A-2b as an output option, document metadata written at creation time,
  a ready-made "Sayfa X / Y" footer.
- **pdfcpu:** the page selection syntax (`1-3,5,!2`, `even`, `odd`, `l-1`) and the watermark/stamp
  parameters (position, scale, opacity, rotation).
- **PDFium:** Chrome-quality rasterizing (thumbnails, previews) and text extraction.
- **Stirling-PDF:** "everything in one tool", but as a service/component without a UI.

## 3. Language: Go

| Criterion | Go | .NET | Node | Rust |
|---|---|---|---|---|
| Single binary CLI + server | ✔ static, `CGO_ENABLED=0` | AOT possible, but the libraries (QuestPDF/PDFsharp) are licensed or not AOT friendly | ✗ needs a runtime | ✔ |
| Low memory | ✔ (~20-40 MB process) | medium (~60-100 MB) | medium | ✔ |
| Chromium control | ✔ chromedp (MIT, pure CDP) | PuppeteerSharp (MIT) | ✔ Playwright | chromiumoxide (less mature) |
| PDF manipulation | ✔ **pdfcpu (Apache-2.0)**, mature | no mature permissive option (PDFsharp AGPL, iText AGPL) | pdf-lib (MIT, slow maintenance) | lopdf (low level) |
| Rasterizing | ✔ go-pdfium WebAssembly (MIT + PDFium BSD), **no cgo** | native PDFium packages | pdf.js + canvas (native) | pdfium-render (native) |
| Multi-arch Docker | ✔ one-line cross compile | ✔ | ✔ | ✔ but slow builds |

**Decision: Go.** The deciding point is that mature, permissively licensed PDF manipulation (pdfcpu) and
rasterizing (PDFium-WASM) both exist in Go without cgo, so the same static binary is built for linux/arm64
and amd64.

## 4. Architecture

```
            ┌──────────── pidief (single Go binary) ─────────┐
CLI ───────▶│                                                │
            │  ops registry  ── each operation: name,        │
HTTP API ──▶│  (one table)      params struct, inputs, Run() │
            │      │                                         │
            │      ├─ chromium (chromedp → headless Chromium,│──▶ chromium-headless-shell
            │      │   virtual origin via Fetch, external    │     (separate process, reused)
            │      │   requests blocked, PrintToPDF)         │
            │      ├─ pdfops (pdfcpu: merge, split, ...)     │
            │      ├─ pdfa   (XMP + sRGB output intent ...)  │
            │      ├─ imgpdf (EXIF orientation, fit, JPEG)   │
            │      ├─ raster (PDFium-WASM → PNG/WebP/JPEG)   │
            │      └─ tmpl   (Handlebars/Mustache + tr-TR)   │
            └────────────────────────────────────────────────┘
```

- **One operation table:** CLI subcommands and `/v1/<operation>` endpoints are generated from the same
  registry; parameters are bound from struct tags to CLI flags, form fields, JSON and the OpenAPI schema,
  so the CLI and the API cannot drift apart.
- **Stateless:** inputs stay in memory; Chromium receives documents from `https://pidief.local/` through
  the Fetch domain, straight from memory, never from disk. Async job results are kept in memory with a TTL.
- **Security:** external network requests are blocked by default (`allowExternal=false`, and the server can
  forbid the parameter), the container runs as a non-root user with a read-only root, request size and
  concurrency are limited.
- **Office to PDF:** out of scope. The only clean option is LibreOffice (MPL-2.0), which would roughly
  quadruple the image. It could become a separate `pidief-office` image later.
- **HEIC:** out of scope. The decoder (libde265) is LGPL and HEVC carries patent risk; clients should send JPEG.
