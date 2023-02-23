#!/bin/bash

set -e -x -u

# makes builds reproducible
export CGO_ENABLED=0

go fmt ./cmd/... ./pkg/... ./test/...
go mod vendor
go mod tidy

# export GOOS=linux GOARCH=amd64
go build -trimpath -o "imgpkg${IMGPKG_BINARY_EXT-}" ./cmd/imgpkg/...
./imgpkg version

echo "Success"
