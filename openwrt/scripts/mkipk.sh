#!/usr/bin/env bash
# Build an OpenWrt-compatible .ipk for modern opkg (OpenWrt 24+/25+, Kwrt, etc.).
#
# Modern format (NOT Debian ar):
#   outer: gzip-compressed ustar containing
#     ./debian-binary
#     ./data.tar.gz
#     ./control.tar.gz
#
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

if [[ -s "$PKG_ROOT/CONTROL/control" ]] && [[ $(tail -c1 "$PKG_ROOT/CONTROL/control" | wc -l) -eq 0 ]]; then
  printf '\n' >>"$PKG_ROOT/CONTROL/control"
fi

# Inner tarballs
(
  cd "$PKG_ROOT/CONTROL"
  "${TAR_BASE[@]}" -cf - . | gzip -n - >"$WORKDIR/control.tar.gz"
)
(
  cd "$WORKDIR/data"
  "${TAR_BASE[@]}" -cf - . | gzip -n - >"$WORKDIR/data.tar.gz"
)

printf '2.0\n' >"$WORKDIR/debian-binary"

# Outer package: gzip(tar of ./debian-binary ./data.tar.gz ./control.tar.gz)
# Matches OpenWrt 24.10+ official package layout (not classic ar/deb).
rm -f "$OUT_IPK"
(
  cd "$WORKDIR"
  "${TAR_BASE[@]}" -cf - ./debian-binary ./data.tar.gz ./control.tar.gz \
    | gzip -n - >"$OUT_IPK"
)

# Sanity checks
if [[ ! -s "$OUT_IPK" ]]; then
  echo "empty output $OUT_IPK" >&2
  exit 1
fi
# Must start with gzip magic, not ar magic
magic=$(od -An -tx1 -N 2 "$OUT_IPK" 2>/dev/null | tr -d ' \n' || true)
if command -v python3 >/dev/null 2>&1; then
  python3 - "$OUT_IPK" <<'PY'
import gzip, sys, tarfile, io
path = sys.argv[1]
raw = open(path, "rb").read(2)
if raw != b"\x1f\x8b":
    raise SystemExit(f"expected gzip magic, got {raw!r}")
data = gzip.decompress(open(path, "rb").read())
tf = tarfile.open(fileobj=io.BytesIO(data), mode="r:")
names = tf.getnames()
need = {"./debian-binary", "./data.tar.gz", "./control.tar.gz"}
if not need.issubset(set(names)):
    # also accept without ./
    alt = {n.lstrip("./") for n in names}
    if not {"debian-binary", "data.tar.gz", "control.tar.gz"}.issubset(alt):
        raise SystemExit(f"unexpected outer members: {names}")
print("ok members:", names)
PY
fi

echo "built $OUT_IPK ($(wc -c <"$OUT_IPK") bytes) [openwrt gzip-tar ipk]"
ls -la "$OUT_IPK"
