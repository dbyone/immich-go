# Build stage — CGO links the prebuilt DuckDB static bindings, which are
# glibc builds: use Debian (bookworm), not Alpine/musl.
FROM golang:1.27-bookworm AS builder
RUN apt-get update && apt-get install -y --no-install-recommends gcc g++ && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/immich-go ./cmd/immich-go

# Runtime stage — ffmpeg powers video poster extraction (optional but
# recommended; metadata works without it via the pure-Go MP4 parser)
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates tzdata ffmpeg \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/immich-go /usr/bin/immich-go
ENV IMMICH_PORT=2283 IMMICH_MEDIA_LOCATION=/data
VOLUME /data
EXPOSE 2283
ENTRYPOINT ["/usr/bin/immich-go"]
