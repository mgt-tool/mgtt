#!/bin/bash
set -e
cd "$(dirname "$0")/.."
mkdir -p bin
go build -o bin/mgtt-provider-kubernetes .
echo "✓ built bin/mgtt-provider-kubernetes"
