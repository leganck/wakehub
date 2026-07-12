#!/usr/bin/env bash
# Build a classic OpenWrt .ipk (ar of debian-binary + control.tar.gz + data.tar.gz).
# Usage: mkipk.sh <package_root> <output.ipk>
# package_root must contain CONTROL/ and data files layout (usr/, etc/, ...).

set -euo pipefail

PKG_ROOT="${1:?package root required}"
OUT_IPK="${2:?output ipk path required}"

# Resolve paths before any cd (ar runs from a temp workdir).
PKG_ROOT="$(cd "$PKG_ROOT" && pwd)"
OUT_DIR="$(cd "$(dirname "$OUT_IPK")" 2>/dev/null && pwd || true)"
if [[ -z "$OUT_DIR" ]]; then
  mkdir -p "$(dirname "$OUT_IPK")"
  OUT_DIR="$(cd "$(dirname "$OUT_IPK")" && pwd)"
fi
OUT_IPK="${OUT_DIR}/$(basename "$OUT_IPK")"

if [[ ! -d "$PKG_ROOT/CONTROL" ]]; then
  echo "missing CONTROL/ in $PKG_ROOT" >&2
  exit 1
fi
if [[ ! -f "$PKG_ROOT/CONTROL/control" ]]; then
  echo "missing CONTROL/control" >&2
  exit 1
fi

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

mkdir -p "$WORKDIR/data"
# Copy everything except CONTROL into data
shopt -s dotglob nullglob
for item in "$PKG_ROOT"/*; do
  base=$(basename "$item")
  if [[ "$base" == "CONTROL" ]]; then
    continue
  fi
  cp -a "$item" "$WORKDIR/data/"
done

# Ensure control scripts are executable
for s in postinst prerm preinst postrm; do
  if [[ -f "$PKG_ROOT/CONTROL/$s" ]]; then
    chmod 755 "$PKG_ROOT/CONTROL/$s"
  fi
done

echo "2.0" >"$WORKDIR/debian-binary"

tar --format=gnu --owner=0 --group=0 --numeric-owner \
  -czf "$WORKDIR/control.tar.gz" -C "$PKG_ROOT/CONTROL" .

tar --format=gnu --owner=0 --group=0 --numeric-owner \
  -czf "$WORKDIR/data.tar.gz" -C "$WORKDIR/data" .

rm -f "$OUT_IPK"

# ipk is an ar archive; write with absolute path after staging members
if command -v ar >/dev/null 2>&1; then
  (
    cd "$WORKDIR"
    ar r "$OUT_IPK" debian-binary control.tar.gz data.tar.gz
  )
else
  echo "ar(1) is required to build ipk" >&2
  exit 1
fi

echo "built $OUT_IPK"
ls -la "$OUT_IPK"
