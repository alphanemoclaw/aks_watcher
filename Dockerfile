# ─── Stage 1: Build ───────────────────────────────────────────────────────────
# Use the official Go image only for compilation; it will NOT ship in the
# final image.
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy dependency manifests first so Docker can cache this layer independently
# of source changes — `go mod download` only reruns when go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and compile a fully static binary.
# -ldflags="-s -w"  strips debug symbols → smaller binary
# CGO_ENABLED=0     no C dependencies → runs on scratch/distroless
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /aks-watcher .

# ─── Stage 2: Runtime ─────────────────────────────────────────────────────────
# distroless/static contains only CA certificates and timezone data.
# No shell, no package manager — minimal attack surface.
# Final image size: ~10 MB.
FROM gcr.io/distroless/static-debian12

COPY --from=builder /aks-watcher /aks-watcher

EXPOSE 8080

ENTRYPOINT ["/aks-watcher"]
