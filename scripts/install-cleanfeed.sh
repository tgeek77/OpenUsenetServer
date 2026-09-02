#!/bin/bash
# Install cleanfeed-ng into a container image (Docker build or Incus setup).
set -euo pipefail

CLEANFEED_NG_VERSION="${CLEANFEED_NG_VERSION:-2026-07-03-rc1}"
INSTALL_ROOT="${CLEANFEED_NG_ROOT:-/usr/local/lib/cleanfeed-ng}"
ETC_DIR="${INSTALL_ROOT}/etc"
FILTER_WRAPPER="${OPENUSENET_FILTER:-/usr/local/lib/openusenet/cleanfeed-filter.pl}"
LOCAL_CONF_SRC="${CLEANFEED_LOCAL_SRC:-}"

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y --no-install-recommends \
	ca-certificates curl unzip perl \
	libdigest-md5-perl libdigest-sha-perl libmime-base64-perl \
	>/dev/null

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

zip="${tmpdir}/cleanfeed-ng.zip"
curl -fsSL -o "$zip" \
	"https://github.com/infybofh/cleanfeed-ng/releases/download/${CLEANFEED_NG_VERSION}/cleanfeed-ng-${CLEANFEED_NG_VERSION}.zip"

unzip -q "$zip" -d "$tmpdir"
src="$(find "$tmpdir" -mindepth 1 -maxdepth 1 -type d | head -1)"
if [[ -z "$src" || ! -f "$src/cleanfeed" ]]; then
	echo "cleanfeed-ng release layout unexpected" >&2
	exit 1
fi

install -d -m 0755 "$INSTALL_ROOT" "$ETC_DIR" "$(dirname "$FILTER_WRAPPER")"
install -m 0755 "$src/cleanfeed" "$INSTALL_ROOT/cleanfeed"
if [[ -f "$src/cleanfeed-admin.pl" ]]; then
	install -m 0755 "$src/cleanfeed-admin.pl" "$INSTALL_ROOT/cleanfeed-admin.pl"
fi
if [[ -d "$src/central-lists/default" ]]; then
	cp -a "$src/central-lists/default/." "$ETC_DIR/"
fi

if [[ -n "$LOCAL_CONF_SRC" && -f "$LOCAL_CONF_SRC" ]]; then
	install -m 0644 "$LOCAL_CONF_SRC" "$ETC_DIR/cleanfeed.local"
elif [[ ! -f "$ETC_DIR/cleanfeed.local" ]]; then
	install -m 0644 "$src/cleanfeed.local.example" "$ETC_DIR/cleanfeed.local"
fi

if [[ -f "${CLEANFEED_FILTER_SRC:-}" ]]; then
	install -m 0755 "$CLEANFEED_FILTER_SRC" "$FILTER_WRAPPER"
fi

perl -c "$INSTALL_ROOT/cleanfeed" >/dev/null
perl -c "$ETC_DIR/cleanfeed.local" >/dev/null
if [[ -x "$INSTALL_ROOT/cleanfeed-admin.pl" ]]; then
	CLEANFEED_CONFIG_DIR="$ETC_DIR" "$INSTALL_ROOT/cleanfeed-admin.pl" --config-dir "$ETC_DIR" --check-config
fi

echo "cleanfeed-ng ${CLEANFEED_NG_VERSION} -> ${INSTALL_ROOT}"
