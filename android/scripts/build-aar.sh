#!/usr/bin/env bash
# Convenience wrapper that drives gomobile bind from the repo root and
# drops the resulting aar into android/app/libs/, so a fresh
# `gradle assembleRelease` picks it up without manual copying.
#
# Required environment:
#   ANDROID_HOME       — Android SDK (containing platforms;android-29+)
#   ANDROID_NDK_HOME   — NDK r26+
#   PATH               — must include $HOME/go/bin so gomobile is callable
#
# Optional environment:
#   FRPDECK_ABI       — comma-separated, default "android" (== all 4 ABIs)
#                       use "android/arm64" for fast iteration
set -euo pipefail

cd "$(dirname "$0")/../.."   # repo root
mkdir -p build android/app/libs

TARGET="${FRPDECK_ABI:-android}"
OUT="build/frpdeckmobile.aar"

echo "→ gomobile bind target=${TARGET}"
gomobile bind \
    -target="${TARGET}" \
    -androidapi=29 \
    -ldflags="-checklinkname=0" \
    -o "${OUT}" \
    ./mobile/frpdeckmobile

cp -f "${OUT}" android/app/libs/frpdeckmobile.aar
echo "✓ android/app/libs/frpdeckmobile.aar refreshed ($(du -h ${OUT} | cut -f1))"
