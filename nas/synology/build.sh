#!/usr/bin/env bash
# build.sh — produce dual-architecture Synology DSM 7 .spk packages.
#
# Output:
#   nas/synology/dist/frpdeck-x86_64-${VERSION}.spk
#   nas/synology/dist/frpdeck-aarch64-${VERSION}.spk
#
# Usage:
#   bash nas/synology/build.sh                   # both archs
#   bash nas/synology/build.sh x86_64            # x86_64 only
#   bash nas/synology/build.sh aarch64           # aarch64 only
#   VERSION=v0.7.0 bash nas/synology/build.sh    # override version stamp
#
# Why a hand-rolled SPK instead of `PkgCreate.py` / spksrc:
#   - PkgCreate.py only ships in Synology's Package Toolkit (DSM-bundled);
#     not available on plain Linux CI runners.
#   - spksrc is a full cross-compile / Makefile farm; overkill when our
#     binary already cross-compiles cleanly via CGO_ENABLED=0 + Go 1.25.
#   - SPK is just an outer tar wrapping a Linux-style tar.gz payload
#     (`package.tgz`) plus a few sidecars (INFO, scripts/, conf/, icons,
#     LICENSE). Python's stdlib `tarfile` covers both layers; we control
#     file modes precisely so cross-arch builds stay reproducible.
#
# DSM 7 contract notes (https://help.synology.com/developer-guide/synology_package/):
#   - SPK outer container is a **plain tar** (not gzip).
#   - INFO must declare `arch="<value>"` matching one of the toolchain
#     names in https://help.synology.com/developer-guide/appendix/platarchs.html
#   - scripts/* live OUTSIDE package.tgz and run as the system caller
#     before the payload is unpacked (preinst, etc.) or after (postinst).
#   - conf/privilege with `run-as: package` makes DSM autocreate user
#     `sc-frpdeck` and run start-stop-status as that user.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PKG_DIR="${ROOT_DIR}/nas/synology/frpdeck"
DIST_DIR="${ROOT_DIR}/nas/synology/dist"
WORK_DIR="$(mktemp -d -t frpdeck-spk-XXXXXX)"
trap 'rm -rf "${WORK_DIR}"' EXIT

VERSION="${VERSION:-0.7.0-1}"
FRP_VERSION="${FRP_VERSION:-v0.68.1}"

# DSM 7 INFO version field requires "[feature]-[build]" form. Strip leading
# 'v' from a git tag and append "-1" if the user didn't already include a
# build number, e.g. v0.7.0 -> 0.7.0-1.
SPK_VERSION="${VERSION#v}"
if [[ "${SPK_VERSION}" != *-* ]]; then
    SPK_VERSION="${SPK_VERSION}-1"
fi

mkdir -p "${DIST_DIR}"

# --- 1. Build frontend dist (shared by both arches; embedded into binary) --

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

# --- 3. Stage layout, build inner package.tgz, then outer .spk ---

pack_one() {
    local arch="$1"      # x86_64 | aarch64
    local goarch="$2"    # amd64  | arm64

    local stage="${WORK_DIR}/${arch}"
    rm -rf "${stage}"
    mkdir -p "${stage}/payload/bin" "${stage}/spk/scripts" "${stage}/spk/conf"

    # 3a. Build the binary into the payload tree.
    go_build "${goarch}" "${stage}/payload/bin/frpdeck-server"
    chmod 0755 "${stage}/payload/bin/frpdeck-server"

    # 3b. Roll inner tar.gz with stable mtime for reproducibility.
    echo "[pack] inner package.tgz (${arch})"
    python3 - "${stage}/payload" "${stage}/spk/package.tgz" <<'PY'
import os, sys, tarfile, time

src, out = sys.argv[1], sys.argv[2]
mtime = int(os.environ.get("SOURCE_DATE_EPOCH", time.time()))

def reset(ti):
    ti.uid = 0
    ti.gid = 0
    ti.uname = ""
    ti.gname = ""
    ti.mtime = mtime
    return ti

with tarfile.open(out, "w:gz") as tf:
    for root, dirs, files in os.walk(src):
        dirs.sort()
        rel = os.path.relpath(root, src)
        # Add the directory itself (preserves layout in absence of files).
        if rel != ".":
            ti = tf.gettarinfo(root, arcname=rel)
            tf.addfile(reset(ti))
        for name in sorted(files):
            full = os.path.join(root, name)
            arc = os.path.relpath(full, src)
            ti = tf.gettarinfo(full, arcname=arc)
            with open(full, "rb") as fh:
                tf.addfile(reset(ti), fh)
PY

    # 3c. Compose outer SPK metadata.
    cp "${PKG_DIR}/PACKAGE_ICON.PNG"     "${stage}/spk/PACKAGE_ICON.PNG"
    cp "${PKG_DIR}/PACKAGE_ICON_256.PNG" "${stage}/spk/PACKAGE_ICON_256.PNG"
    cp "${PKG_DIR}/LICENSE"              "${stage}/spk/LICENSE"
    cp -r "${PKG_DIR}/conf/."            "${stage}/spk/conf/"
    cp -r "${PKG_DIR}/scripts/."         "${stage}/spk/scripts/"

    # Render INFO with arch + version.
    sed -e "s/@@ARCH@@/${arch}/g" \
        -e "s/@@VERSION@@/${SPK_VERSION}/g" \
        "${PKG_DIR}/INFO.template" > "${stage}/spk/INFO"

    # 3d. Roll the outer .spk (plain tar — Synology Package Center loads it
    # directly without gzip).
    local out="${DIST_DIR}/frpdeck-${arch}-${SPK_VERSION}.spk"
    rm -f "${out}"
    echo "[pack] outer .spk -> ${out}"
    python3 - "${stage}/spk" "${out}" <<'PY'
import os, sys, tarfile, time

src, out = sys.argv[1], sys.argv[2]
mtime = int(os.environ.get("SOURCE_DATE_EPOCH", time.time()))

EXEC_PATHS = (
    "scripts/",  # all DSM lifecycle scripts must be executable
)

def reset(ti, arc):
    ti.uid = 0
    ti.gid = 0
    ti.uname = ""
    ti.gname = ""
    ti.mtime = mtime
    # Force 0755 on shell scripts in scripts/, 0644 on everything else.
    if any(arc.startswith(p) for p in EXEC_PATHS) and not ti.isdir():
        ti.mode = 0o755
    elif ti.isdir():
        ti.mode = 0o755
    else:
        ti.mode = 0o644
    return ti

with tarfile.open(out, "w") as tf:
    # DSM expects INFO as the very first entry so it can stat the archive
    # without a full scan; mimic that ordering.
    paths = []
    for root, dirs, files in os.walk(src):
        dirs.sort()
        for name in sorted(files):
            full = os.path.join(root, name)
            arc = os.path.relpath(full, src)
            paths.append((arc, full))
    paths.sort(key=lambda p: (p[0] != "INFO", p[0]))
    for arc, full in paths:
        ti = tf.gettarinfo(full, arcname=arc)
        with open(full, "rb") as fh:
            tf.addfile(reset(ti, arc), fh)
PY

    ls -lh "${out}"
}

# --- 4. Drive the matrix ---

want_x86=true
want_arm=true
case "${1:-}" in
    x86_64)  want_arm=false ;;
    aarch64) want_x86=false ;;
    "")      ;;
    *)
        echo "build.sh: unknown arch '$1' (expected x86_64 | aarch64)" >&2
        exit 1
        ;;
esac

frontend_build

if ${want_x86}; then pack_one x86_64  amd64; fi
if ${want_arm}; then pack_one aarch64 arm64; fi

echo "[done] artifacts under ${DIST_DIR}"
