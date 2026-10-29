#!/bin/sh
# Rebuild Python bindings from the vendored, community reverse-engineered protos.
set -eu
cd "$(dirname "$0")/.."
PROTO_DIR=vendor/ProPresenter7-Proto/autogen-proto
mkdir -p gen
protoc -I "$PROTO_DIR" --python_out=gen "$PROTO_DIR"/*.proto 2>&1 | grep -v 'is unused' || true
