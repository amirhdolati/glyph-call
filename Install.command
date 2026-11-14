#!/bin/bash
set -eu
cd -- "$(dirname -- "$0")"
if [ ! -x bin/glyph-call ]; then
    bash build.sh
fi
bin/glyph-call --version
echo "Ready. Run bash Start.command."
