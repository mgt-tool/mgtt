FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/mgt-tool/mgtt/internal/cli.version=${VERSION}" -o /mgtt ./cmd/mgtt

FROM alpine:3.20
RUN apk add --no-cache ca-certificates git bash
RUN adduser -D -h /home/mgtt mgtt
COPY --from=builder /mgtt /usr/local/bin/mgtt
ENV MGTT_HOME=/data
# MCP sidecar default port. Override with `mgtt mcp serve --listen :PORT`.
EXPOSE 8080
USER mgtt
WORKDIR /workspace
ENTRYPOINT ["mgtt"]
