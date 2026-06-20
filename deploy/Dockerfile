# syntax=docker/dockerfile:1

# ── Stage 1: frontend (React + TS + Vite, pnpm) ─────────────────────────
FROM node:22-alpine AS frontend
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
FROM golang:1.26-alpine AS build
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
