#!/bin/bash

set -e -x -u

# makes builds reproducible
export CGO_ENABLED=0

/tmp/go/bin/go fmt ./cmd/... ./pkg/... ./test/...
/tmp/go/bin/go mod vendor
/tmp/go/bin/go mod tidy

# export GOOS=linux GOARCH=amd64
/tmp/go/bin/go build -trimpath -o "imgpkg${IMGPKG_BINARY_EXT-}" ./cmd/imgpkg/...
./imgpkg version

echo "Success"
