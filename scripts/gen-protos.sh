#!/bin/sh
# Generate Go bindings for rv.data.Presentation and its imports from the vendored community protos.
set -eu
cd "$(dirname "$0")/.."
ROOT=$(pwd)
PROTO_DIR=third_party/ProPresenter7-Proto/autogen-proto
OUT=internal/rvpb
PKG=probundle/internal/rvpb
# Transitive imports of presentation.proto (protoc --include_imports), without google/protobuf.
FILES="customOptions color alphaType digitalAudio uuid effects url fileProperties font intRange graphicsData
background collectionElementType input hotKey layers version applicationInfo templateIdentification timers
messages alignmentGuide slide propSlide presentationSlide stage action cue groups musicKeyScale rvtimestamp presentation"
MAPS=""
SRCS=""
for f in $FILES; do
  MAPS="$MAPS --go_opt=M$f.proto=$PKG;rvpb"
  SRCS="$SRCS $f.proto"
done
rm -rf "$OUT" && mkdir -p "$OUT"
PATH="$(go env GOPATH)/bin:$PATH"
cd "$PROTO_DIR"
# shellcheck disable=SC2086
protoc -I . --go_out="$ROOT/$OUT" --go_opt=paths=source_relative $MAPS $SRCS 2>&1 | grep -v 'is unused' || true
