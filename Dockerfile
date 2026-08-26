# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/immich-go ./cmd/immich-go

# Runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /out/immich-go /usr/bin/immich-go
ENV IMMICH_PORT=2283 IMMICH_MEDIA_LOCATION=/data
VOLUME /data
EXPOSE 2283
ENTRYPOINT ["/usr/bin/immich-go"]
