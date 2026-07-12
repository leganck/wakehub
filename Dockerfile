# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/wakehub-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 wakehub
WORKDIR /app
COPY --from=build /out/wakehub-server /app/wakehub-server
RUN mkdir -p /app/data && chown -R wakehub:wakehub /app
USER wakehub
EXPOSE 8080
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/wakehub-server", "-config", "/app/data/config.json", "-listen", ":8080"]
