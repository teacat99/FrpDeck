#!/usr/bin/env bash
# build.sh — produce dual-architecture FrpDeck .fpk packages for fnOS.
#
# Output:
#   nas/fnos/dist/com.teacat.frpdeck-${VERSION}-x86.fpk
#   nas/fnos/dist/com.teacat.frpdeck-${VERSION}-arm.fpk
#
# Usage:
#   bash nas/fnos/build.sh                 # build both architectures
#   bash nas/fnos/build.sh x86             # only x86_64
#   bash nas/fnos/build.sh arm             # only aarch64
#   VERSION=v0.7.0 bash nas/fnos/build.sh  # override version stamp
#
# Why a hand-rolled zip instead of `fnpack build`:
#   .fpk is just a zip with a fixed layout. fnpack only runs on fnOS itself,
#   which is not part of our CI environment. Producing the zip here lets us
#   keep amd64 + arm64 cross builds inside one Linux host (CGO_ENABLED=0,
#   no toolchain juggling) and still pass `fnpack build` later if/when an
#   audit tool requires it.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PKG_DIR="${ROOT_DIR}/nas/fnos/frpdeck"
DIST_DIR="${ROOT_DIR}/nas/fnos/dist"
WORK_DIR="$(mktemp -d -t frpdeck-fpk-XXXXXX)"
trap 'rm -rf "${WORK_DIR}"' EXIT

VERSION="${VERSION:-$(grep -E '^version' "${PKG_DIR}/manifest" | head -n1 | awk -F= '{print $2}' | xargs)}"
FRP_VERSION="${FRP_VERSION:-v0.68.1}"

if [[ -z "${VERSION}" ]]; then
    echo "build.sh: failed to resolve VERSION (manifest version= line missing?)" >&2
    exit 1
fi

# Strip leading 'v' so manifest version=0.7.0 stays semver while CI can
# still pass VERSION=v0.7.0 sourced from github.ref_name. The leading 'v'
# is preserved in the binary's main.appVersion (frpdeck-server version
# output keeps the 'v' for parity with `git describe`).
MANIFEST_VERSION="${VERSION#v}"

mkdir -p "${DIST_DIR}"

# --- 1. Build frontend dist (shared by all arches; embedded into binary) ---

frontend_build() {
    if [[ -n "${FRPDECK_SKIP_FRONTEND_BUILD:-}" ]]; then
        echo "[build] skipping frontend build (FRPDECK_SKIP_FRONTEND_BUILD=${FRPDECK_SKIP_FRONTEND_BUILD})"
        return
    fi
    echo "[build] frontend npm run build"
    (
        cd "${ROOT_DIR}/frontend"
        npm ci --no-audit --no-fund
        npm run build
    )
}

# --- 2. Cross-build frpdeck-server for one GOARCH ---

go_build() {
    local goarch="$1"   # amd64 | arm64
    local out="$2"
    echo "[build] go build linux/${goarch} -> ${out}"
    (
        cd "${ROOT_DIR}"
        CGO_ENABLED=0 GOOS=linux GOARCH="${goarch}" go build \
            -trimpath \
            -ldflags "-s -w \
                -X 'github.com/teacat99/FrpDeck/internal/frpcd.BundledFrpVersion=${FRP_VERSION}' \
                -X 'main.appVersion=${VERSION}'" \
            -o "${out}" \
            ./cmd/server/
    )
}

# --- 3. Stage package layout into a temp dir, swap manifest platform, zip --

pack_one() {
    local arch_label="$1"   # x86 | arm
    local goarch="$2"       # amd64 | arm64

    local stage="${WORK_DIR}/${arch_label}"
    rm -rf "${stage}"
    mkdir -p "${stage}/app/server"

    cp -r "${PKG_DIR}/." "${stage}/"

    # Build the binary directly into the staged tree.
    go_build "${goarch}" "${stage}/app/server/frpdeck-server"
    chmod 0755 "${stage}/app/server/frpdeck-server"

    # Rewrite manifest platform line in-place. The committed manifest leaves
    # platform=x86 by default; we patch it for the arm artifact.
    sed -i -E "s/^platform[[:space:]]*=.*/platform              = ${arch_label}/" "${stage}/manifest"

    # Sync manifest version with the resolved VERSION so the artifact
    # filename, manifest version, and binary appVersion never drift apart.
    sed -i -E "s/^version[[:space:]]*=.*/version               = ${MANIFEST_VERSION}/" "${stage}/manifest"

    # cmd/* + main/install_* shell hooks must keep their executable bit.
    chmod 0755 "${stage}/cmd"/*

    local out="${DIST_DIR}/com.teacat.frpdeck-${MANIFEST_VERSION}-${arch_label}.fpk"
    rm -f "${out}"

    echo "[pack] ${arch_label} -> ${out}"
    # Use python's zipfile so we don't have to depend on the host having
    # the `zip` CLI. We must set external_attr=mode<<16 manually so cmd/*
    # shell hooks keep their executable bit on the fnOS side.
    python3 - "${stage}" "${out}" <<'PY'
import os, sys, zipfile

stage, out = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as zf:
    for root, dirs, files in os.walk(stage):
        dirs.sort()
        for name in sorted(files):
            fp = os.path.join(root, name)
            arc = os.path.relpath(fp, stage)
            st = os.stat(fp)
            zi = zipfile.ZipInfo(arc)
            zi.compress_type = zipfile.ZIP_DEFLATED
            zi.external_attr = (st.st_mode & 0xFFFF) << 16
            with open(fp, 'rb') as src:
                zf.writestr(zi, src.read())
PY

    ls -lh "${out}"
}

# --- 4. Drive the matrix ---

want_x86=true
want_arm=true
case "${1:-}" in
    x86) want_arm=false ;;
    arm) want_x86=false ;;
    "")  ;;
    *)
        echo "build.sh: unknown arch '$1' (expected x86 | arm)" >&2
        exit 1
        ;;
esac

frontend_build

if ${want_x86}; then pack_one x86 amd64; fi
if ${want_arm}; then pack_one arm arm64; fi

echo "[done] artifacts under ${DIST_DIR}"
