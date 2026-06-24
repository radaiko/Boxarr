# syntax=docker/dockerfile:1

# ── Stage 1: frontend (React + TS + Vite, pnpm) ─────────────────────────
# --platform=$BUILDPLATFORM: build on the native runner (amd64), never under QEMU.
# The dist is static + arch-independent, so it's built once and reused for every
# target platform — no emulated pnpm/vite runs.
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /web
# corepack pins pnpm from web/package.json "packageManager" (FR-SEC-6).
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/.npmrc ./
# --frozen-lockfile fails on lockfile drift (FR-SEC-1). pnpm 10 blocks
# dependency build scripts by default; only onlyBuiltDependencies (esbuild) run.
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build            # vite build -> /web/dist (static, embedded — FR-SEC-5)

# ── Stage 2: Go build (embeds the built frontend) ───────────────────────
# --platform=$BUILDPLATFORM: the Go toolchain runs natively on the runner (amd64)
# and CROSS-compiles to ${TARGETARCH} (CGO disabled, modernc sqlite is pure Go).
# This avoids compiling arm64 under QEMU emulation — the slowest part of the build
# (5-10x) — so every target arch builds at native speed. go.mod download + build
# also run once, not once per emulated platform.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Built SPA lands where //go:embed all:dist expects it (internal/web).
COPY --from=frontend /web/dist ./internal/web/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/boxarr ./cmd/boxarr

# ── Stage 3: runtime (distroless, nonroot) ──────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/boxarr /boxarr
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=60s --timeout=5s --retries=3 \
    CMD ["/boxarr", "healthcheck"]
ENTRYPOINT ["/boxarr"]
