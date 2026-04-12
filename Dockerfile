FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /mgtt ./cmd/mgtt

# Build kubernetes provider (separate Go module)
WORKDIR /src/providers/kubernetes
RUN go mod download 2>/dev/null; CGO_ENABLED=0 go build -ldflags="-s -w" -o /mgtt-provider-kubernetes .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates kubectl aws-cli bash

COPY --from=builder /mgtt /usr/local/bin/mgtt
COPY --from=builder /mgtt-provider-kubernetes /usr/local/bin/mgtt-provider-kubernetes
# Copy provider YAML to known location
COPY providers/ /usr/share/mgtt/providers/

WORKDIR /workspace
ENV MGTT_HOME=/usr/share/mgtt
ENTRYPOINT ["mgtt"]
