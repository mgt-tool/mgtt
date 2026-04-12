FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /mgtt ./cmd/mgtt

FROM alpine:3.20

RUN apk add --no-cache ca-certificates kubectl aws-cli bash

COPY --from=builder /mgtt /usr/local/bin/mgtt

WORKDIR /workspace
ENTRYPOINT ["mgtt"]
