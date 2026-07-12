#!/usr/bin/env bash
# Cross-compile wakehub-server and build OpenWrt .ipk packages.
#
# Usage:
#   ./openwrt/scripts/build-ipk.sh
#   VERSION=0.1.1 ./openwrt/scripts/build-ipk.sh
#   ARCHES="mipsel_24kc x86_64" ./openwrt/scripts/build-ipk.sh
#
# Env:
#   VERSION   package version (default: git describe or 0.0.0-dev)
#   OUT_DIR   output directory (default: dist/openwrt)
#   ARCHES    space-separated OpenWrt arch list
#   SKIP_LUCI set to 1 to skip luci-app-wakehub_all.ipk

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPTS="$(cd "$(dirname "$0")" && pwd)"
OUT_DIR="${OUT_DIR:-$ROOT/dist/openwrt}"
SKIP_LUCI="${SKIP_LUCI:-0}"

if [[ -n "${VERSION:-}" ]]; then
  :
elif git -C "$ROOT" describe --tags --match 'v*' --dirty 2>/dev/null | grep -q .; then
  VERSION=$(git -C "$ROOT" describe --tags --match 'v*' --dirty | sed 's/^v//')
else
  VERSION="0.0.0-dev.$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
fi

# OpenWrt architecture → Go build env
# Format: openwrt_arch|GOARCH|extra_env (comma-separated KEY=VAL)
DEFAULT_ARCHES=(
  "mipsel_24kc|mipsle|GOMIPS=softfloat"
  "mips_24kc|mips|GOMIPS=softfloat"
  "aarch64_generic|arm64|"
  "arm_cortex-a7|arm|GOARM=7"
  "x86_64|amd64|"
  "riscv64_generic|riscv64|"
)

if [[ -n "${ARCHES:-}" ]]; then
  SELECTED=()
  for want in $ARCHES; do
    found=
    for entry in "${DEFAULT_ARCHES[@]}"; do
      oarch="${entry%%|*}"
      if [[ "$oarch" == "$want" ]]; then
        SELECTED+=("$entry")
        found=1
        break
      fi
    done
    if [[ -z "$found" ]]; then
      echo "unknown ARCH: $want (known: ${DEFAULT_ARCHES[*]%%|*})" >&2
      exit 1
    fi
  done
else
  SELECTED=("${DEFAULT_ARCHES[@]}")
fi

echo "==> VERSION=$VERSION"
echo "==> OUT_DIR=$OUT_DIR"
mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR"/wakehub_*.ipk "$OUT_DIR"/luci-app-wakehub_*.ipk 2>/dev/null || true

# Allow override (e.g. GO=/usr/local/go/bin/go or Windows go.exe under WSL)
GO_BIN="${GO:-go}"

build_go() {
  local goarch="$1"
  local extra="$2"
  local outbin="$3"

  (
    cd "$ROOT"
    export CGO_ENABLED=0
    export GOOS=linux
    export GOARCH="$goarch"
    # Prefer modern toolchain when host Go is older than go.mod
    export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
    # clear possibly sticky arch vars
    unset GOARM GOMIPS GOMIPS64 2>/dev/null || true
    if [[ -n "$extra" ]]; then
      # shellcheck disable=SC2086
      export ${extra//,/ }
    fi
    echo "    $GO_BIN build GOARCH=$GOARCH ${extra}"
    "$GO_BIN" build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o "$outbin" ./cmd/server
  )
}

stage_wakehub() {
  local stage="$1"
  local binary="$2"
  local oarch="$3"

  rm -rf "$stage"
  mkdir -p "$stage/CONTROL"
  mkdir -p "$stage/usr/bin"
  mkdir -p "$stage/usr/libexec"
  mkdir -p "$stage/etc/init.d"
  mkdir -p "$stage/etc/config"
  mkdir -p "$stage/etc/wakehub"

  cp -a "$ROOT/openwrt/wakehub/files/." "$stage/"
  install -m 0755 "$binary" "$stage/usr/bin/wakehub-server"
  chmod 0755 "$stage/etc/init.d/wakehub"
  chmod 0755 "$stage/usr/libexec/wakehub-uci-sync"

  sed -e "s/__VERSION__/${VERSION}/g" -e "s/__ARCH__/${oarch}/g" \
    "$ROOT/openwrt/wakehub/CONTROL/control.in" >"$stage/CONTROL/control"
  cp "$ROOT/openwrt/wakehub/CONTROL/conffiles" "$stage/CONTROL/conffiles"
  install -m 0755 "$ROOT/openwrt/wakehub/CONTROL/postinst" "$stage/CONTROL/postinst"
  install -m 0755 "$ROOT/openwrt/wakehub/CONTROL/prerm" "$stage/CONTROL/prerm"
}

stage_luci() {
  local stage="$1"

  rm -rf "$stage"
  mkdir -p "$stage/CONTROL"
  cp -a "$ROOT/openwrt/luci-app-wakehub/files/." "$stage/"

  sed -e "s/__VERSION__/${VERSION}/g" -e "s/__ARCH__/all/g" \
    "$ROOT/openwrt/luci-app-wakehub/CONTROL/control.in" >"$stage/CONTROL/control"
  install -m 0755 "$ROOT/openwrt/luci-app-wakehub/CONTROL/postinst" "$stage/CONTROL/postinst"
}

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

echo "==> Building wakehub ipks"
for entry in "${SELECTED[@]}"; do
  oarch="${entry%%|*}"
  rest="${entry#*|}"
  goarch="${rest%%|*}"
  extra="${rest#*|}"
  if [[ "$extra" == "$goarch" ]]; then
    extra=""
  fi

  echo "--> $oarch (GOARCH=$goarch)"
  bin="$WORKDIR/wakehub-server-$oarch"
  build_go "$goarch" "$extra" "$bin"

  stage="$WORKDIR/pkg-$oarch"
  stage_wakehub "$stage" "$bin" "$oarch"
  ipk="$OUT_DIR/wakehub_${VERSION}_${oarch}.ipk"
  bash "$SCRIPTS/mkipk.sh" "$stage" "$ipk"
done

if [[ "$SKIP_LUCI" != "1" ]]; then
  echo "==> Building luci-app-wakehub (all)"
  stage="$WORKDIR/luci"
  stage_luci "$stage"
  ipk="$OUT_DIR/luci-app-wakehub_${VERSION}_all.ipk"
  bash "$SCRIPTS/mkipk.sh" "$stage" "$ipk"
fi

echo "==> SHA256SUMS"
(
  cd "$OUT_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ./*.ipk >SHA256SUMS
  else
    shasum -a 256 ./*.ipk >SHA256SUMS
  fi
  cat SHA256SUMS
)

echo "==> Done. Packages in $OUT_DIR"
