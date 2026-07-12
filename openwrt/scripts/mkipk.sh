#!/usr/bin/env bash
# Build an OpenWrt-compatible .ipk (same layout as OpenWrt ipkg-build).
# Usage: mkipk.sh <package_root> <output.ipk>
# package_root must contain CONTROL/ and data layout (usr/, etc/, ...).

set -euo pipefail

PKG_ROOT="${1:?package root required}"
OUT_IPK="${2:?output ipk path required}"

PKG_ROOT="$(cd "$PKG_ROOT" && pwd)"
OUT_DIR="$(dirname "$OUT_IPK")"
mkdir -p "$OUT_DIR"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
OUT_IPK="${OUT_DIR}/$(basename "$OUT_IPK")"

if [[ ! -d "$PKG_ROOT/CONTROL" ]]; then
  echo "missing CONTROL/ in $PKG_ROOT" >&2
  exit 1
fi
if [[ ! -f "$PKG_ROOT/CONTROL/control" ]]; then
  echo "missing CONTROL/control" >&2
  exit 1
fi

# OpenWrt/busybox opkg is picky: prefer ustar + gzip -n (no GNU tar extensions).
TAR_BASE=(tar --format=ustar --numeric-owner --owner=0 --group=0)

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

mkdir -p "$WORKDIR/data"
shopt -s dotglob nullglob
for item in "$PKG_ROOT"/*; do
  base=$(basename "$item")
  if [[ "$base" == "CONTROL" ]]; then
    continue
  fi
  cp -a "$item" "$WORKDIR/data/"
done

for s in postinst prerm preinst postrm; do
  if [[ -f "$PKG_ROOT/CONTROL/$s" ]]; then
    chmod 755 "$PKG_ROOT/CONTROL/$s"
  fi
done

# Ensure control ends with a newline (opkg control parser)
if [[ -s "$PKG_ROOT/CONTROL/control" ]] && [[ $(tail -c1 "$PKG_ROOT/CONTROL/control" | wc -l) -eq 0 ]]; then
  printf '\n' >>"$PKG_ROOT/CONTROL/control"
fi

# Build control.tar.gz / data.tar.gz like OpenWrt: tar | gzip -n
(
  cd "$PKG_ROOT/CONTROL"
  "${TAR_BASE[@]}" -cf - . | gzip -n - >"$WORKDIR/control.tar.gz"
)
(
  cd "$WORKDIR/data"
  "${TAR_BASE[@]}" -cf - . | gzip -n - >"$WORKDIR/data.tar.gz"
)

printf '2.0\n' >"$WORKDIR/debian-binary"

# Assemble ar archive (member order: debian-binary, control.tar.gz, data.tar.gz)
rm -f "$OUT_IPK"
(
  cd "$WORKDIR"
  # -c create, -r replace/insert, avoid thin archives
  if ar cqrD "$OUT_IPK" debian-binary control.tar.gz data.tar.gz 2>/dev/null; then
    :
  elif ar cqr "$OUT_IPK" debian-binary control.tar.gz data.tar.gz 2>/dev/null; then
    :
  else
    ar cr "$OUT_IPK" debian-binary control.tar.gz data.tar.gz
  fi
)

# Sanity: must be a readable ar with 3 members
mapfile -t members < <(ar t "$OUT_IPK")
if [[ ${#members[@]} -lt 3 ]]; then
  echo "invalid ipk (ar members: ${members[*]-none})" >&2
  exit 1
fi
if [[ "${members[0]}" != *debian-binary* || "${members[1]}" != *control.tar.gz* || "${members[2]}" != *data.tar.gz* ]]; then
  echo "unexpected ar member order: ${members[*]}" >&2
  exit 1
fi

echo "built $OUT_IPK ($(wc -c <"$OUT_IPK") bytes)"
ls -la "$OUT_IPK"
