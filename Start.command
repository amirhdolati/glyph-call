#!/bin/bash
set -eu
cd -- "$(dirname -- "$0")"
if [ ! -x bin/glyph-call ]; then
    bash Install.command
fi
exec bin/glyph-call menu
