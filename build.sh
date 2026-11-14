#!/bin/bash
set -euo pipefail
task_root="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$task_root"
if ! command -v go >/dev/null 2>&1; then
    echo "Building from source needs Go 1.26+; prebuilt macOS bundles need no Go or Python."
    exit 1
fi
export CGO_ENABLED=1

mkdir -p bin .build
task_link_flags="-s -w"
if [ "$(uname -s)" = Darwin ]; then
    export MACOSX_DEPLOYMENT_TARGET=14.0
    export CGO_CFLAGS="${CGO_CFLAGS:--O2 -g} -mmacosx-version-min=14.0"
    export CGO_LDFLAGS="${CGO_LDFLAGS:--O2 -g} -mmacosx-version-min=14.0"
    task_plist_dir="$(mktemp -d /private/tmp/glyph-call-link.XXXXXX)"
    trap 'rm -rf "$task_plist_dir"' EXIT
    cp internal/devices/Info.plist "$task_plist_dir/Info.plist"
    task_link_flags="$task_link_flags -linkmode=external -extldflags=-Wl,-sectcreate,__TEXT,__info_plist,$task_plist_dir/Info.plist"
fi
# An inherited GOROOT can point at a different Go installation. Resolve the
# toolchain from the selected go executable, without changing global settings.
if [ "${1:-}" = --universal ]; then
    if [ "$(uname -s)" != Darwin ]; then
        echo "Universal bundles are built on macOS."
        exit 1
    fi
    for task_arch in arm64 amd64; do
        env -u GOROOT CGO_ENABLED=1 GOARCH="$task_arch" \
            go build -trimpath -ldflags "$task_link_flags" -o ".build/glyph-call-$task_arch" ./cmd/glyph-call
    done
    task_output=.build/glyph-call-universal
    lipo -create .build/glyph-call-arm64 .build/glyph-call-amd64 -output "$task_output"
else
    task_output=.build/glyph-call-native
    env -u GOROOT \
        go build -trimpath -ldflags "$task_link_flags" -o "$task_output" ./cmd/glyph-call
fi
if [ "$(uname -s)" = Darwin ] && command -v codesign >/dev/null 2>&1; then
    codesign --force --sign - "$task_output"
fi
mv -f "$task_output" bin/glyph-call
bin/glyph-call --version
