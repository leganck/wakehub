# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
# expect build context to include bemfa-go as sibling via compose context tricks is hard;
# copy module files then source. For simple build, vendor or use replace with copied module.
COPY go.mod go.sum ./
COPY . .
# if replace points to ../bemfa-go, copy it in
COPY bemfa-go /bemfa-go
RUN sed -i 's|=> ../bemfa-go|=> /bemfa-go|' go.mod && go mod tidy && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/wakehub-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/wakehub-server /app/wakehub-server
RUN mkdir -p /app/data
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["/app/wakehub-server", "-config", "/app/data/config.json", "-listen", ":8080"]
