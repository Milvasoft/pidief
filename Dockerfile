# syntax=docker/dockerfile:1.7
# pidief: Go binary + chromium-headless-shell on Debian slim, non-root. linux/amd64 and linux/arm64.

FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
# CGO off: PDFium and libwebp run as WebAssembly / transpiled Go, so the binary is static.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -tags nodynamic -trimpath \
      -ldflags="-s -w -X github.com/milvasoft/pidief/internal/server.Version=${VERSION}" \
      -o /out/pidief ./cmd/pidief

FROM debian:trixie-slim
# chromium-headless-shell: the print engine without the desktop browser UI (smaller than chromium).
# fonts: Onest (embedded in the binary too, installed here so header/footer templates can use it),
# DejaVu and Liberation as metric-compatible fallbacks. tini reaps Chromium's child processes.
RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium-headless-shell fonts-dejavu-core fonts-liberation2 fontconfig tini ca-certificates \
 && rm -rf /var/lib/apt/lists/* /usr/share/doc/* /usr/share/man/* /usr/share/locale/* /var/cache/debconf/*-old
COPY internal/assets/fonts/Onest-*.ttf internal/assets/fonts/OFL.txt /usr/share/fonts/truetype/onest/
RUN fc-cache -f \
 && useradd --system --uid 10001 --user-group --home-dir /home/pidief --create-home --shell /usr/sbin/nologin pidief
COPY --from=build /out/pidief /usr/local/bin/pidief

ENV PIDIEF_ADDR=:3000 \
    PIDIEF_CHROMIUM_PATH=/usr/bin/chromium-headless-shell \
    PIDIEF_CHROMIUM_NO_SANDBOX=true \
    HOME=/home/pidief \
    TMPDIR=/tmp
USER 10001:10001
WORKDIR /home/pidief
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["pidief", "healthcheck"]
ENTRYPOINT ["/usr/bin/tini", "--", "pidief"]
CMD ["serve"]
