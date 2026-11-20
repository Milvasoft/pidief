# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `examples/annual-report`: a six-page report showing a full-bleed cover, JavaScript charts, a landscape
  page in a portrait document, a long table, stamped page numbers, a seal image and PDF/A-2b.

## [0.1.1] - 2026-11-19

### Added

- Logo (`assets/logo`), used as the icon of the `Pidief.Client` NuGet package.

### Fixed

- A Chromium process that never answers now fails start-up after 60 seconds instead of hanging readiness
  checks and requests.
- Recycling Chromium no longer mixes the running conversions of the old and the new process.
- On macOS, the start-up error explains that "Chrome for Testing" often cannot start its helper processes
  and suggests chrome-headless-shell or Google Chrome.

### Changed

- CI: GitHub Actions on Node 24 releases; browser tests run on Linux amd64/arm64, Windows and macOS.

## [0.1.0] - 2026-11-19

### Added

- CLI and HTTP API generated from one operation registry.
- HTML and template to PDF with headless Chromium: paper, margins, orientation, header/footer, page numbers,
  embedded Onest font, wait conditions, external resource blocking, timeouts, PDF/A-2b, metadata.
- Handlebars/Mustache template engine with Turkish formatting helpers, and an HTML preview.
- Images to PDF with EXIF orientation, fitting, DPI cap and compression.
- Merge, split, select/delete/rotate pages, watermark, stamp, compress, PDF/A-2b, encrypt, decrypt, metadata.
- Render pages to PNG/JPEG/WebP, document info and text extraction (PDFium WebAssembly).
- API keys, request size and concurrency limits, queue, async jobs, health checks, Prometheus metrics, OpenAPI 3.1.
- Docker image for linux/amd64 and linux/arm64.
- `Pidief.Client` .NET client with typed methods, timeouts and retries.

[Unreleased]: https://github.com/Milvasoft/pidief/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/Milvasoft/pidief/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Milvasoft/pidief/releases/tag/v0.1.0
