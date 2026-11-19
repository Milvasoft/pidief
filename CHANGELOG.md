# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Logo (`assets/logo`), used as the icon of the `Pidief.Client` NuGet package.

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

[Unreleased]: https://github.com/Milvasoft/pidief/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Milvasoft/pidief/releases/tag/v0.1.0
