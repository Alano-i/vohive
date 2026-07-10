#!/bin/sh
set -eu

APP_NAME="vohive"
REPO="${VOHIVE_REPO:-Alano-i/vohive}"
VERSION="${VOHIVE_VERSION:-}"

usage() {
	cat <<EOF
Usage:
  sudo sh scripts/install.sh [--version vX.Y.Z] [--repo owner/repo]

Environment:
  VOHIVE_VERSION   Release tag to install. Defaults to latest GitHub release.
  VOHIVE_REPO      GitHub repo. Defaults to ${REPO}.

One-line install:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | sudo sh
EOF
}

die() {
	echo "error: $*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
		-h|--help)
			usage
			exit 0
			;;
		--version)
			[ $# -ge 2 ] || die "--version requires a value"
			VERSION="$2"
			shift 2
			;;
		--repo)
			[ $# -ge 2 ] || die "--repo requires a value"
			REPO="$2"
			shift 2
			;;
		*)
			die "unknown argument: $1"
			;;
	esac
done

require_root() {
	if [ "$(id -u)" -ne 0 ]; then
		die "please run as root, for example: curl -fsSL ... | sudo sh"
	fi
}

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64)
			echo "amd64"
			;;
		aarch64|arm64)
			echo "arm64"
			;;
		armv7l|armv7*|armv6l|armhf)
			echo "armv7"
			;;
		*)
			die "unsupported architecture: $(uname -m)"
			;;
	esac
}

install_bootstrap_packages() {
	if command -v apt-get >/dev/null 2>&1; then
		apt-get update
		DEBIAN_FRONTEND=noninteractive apt-get install -y \
			ca-certificates curl tzdata kmod udev usbutils
	fi
}

fetch_stdout() {
	url="$1"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO- "$url"
	else
		die "curl or wget is required"
	fi
}

download() {
	url="$1"
	out="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fL --retry 3 --retry-delay 2 -o "$out" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -O "$out" "$url"
	else
		die "curl or wget is required"
	fi
}

try_download() {
	url="$1"
	out="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fsL --retry 2 --retry-delay 1 -o "$out" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$out" "$url"
	else
		return 1
	fi
}

latest_version() {
	json="$(fetch_stdout "https://api.github.com/repos/${REPO}/releases/latest")"
	tag="$(printf '%s\n' "$json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
	[ -n "$tag" ] || die "unable to resolve latest release for ${REPO}"
	echo "$tag"
}

download_installer() {
	version="$1"
	out="$2"
	raw_tag="https://raw.githubusercontent.com/${REPO}/${version}/scripts/install-debian-binary.sh"
	release_asset="https://github.com/${REPO}/releases/download/${version}/install-debian-binary.sh"
	raw_main="https://raw.githubusercontent.com/${REPO}/main/scripts/install-debian-binary.sh"

	if try_download "$raw_tag" "$out"; then
		return
	fi
	if try_download "$release_asset" "$out"; then
		return
	fi
	if try_download "$raw_main" "$out"; then
		return
	fi
	die "unable to download installer script"
}

print_access_hint() {
	ip="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
	if [ -n "$ip" ]; then
		echo "Web: http://${ip}:7575"
	else
		echo "Web: http://<server-ip>:7575"
	fi
}

main() {
	require_root
	if [ ! -f /etc/debian_version ]; then
		echo "warning: /etc/debian_version not found; continuing anyway" >&2
	fi

	install_bootstrap_packages

	if [ -z "$VERSION" ]; then
		VERSION="$(latest_version)"
	fi
	arch="$(detect_arch)"
	asset="vohive_${VERSION}_linux_${arch}"
	base="https://github.com/${REPO}/releases/download/${VERSION}"

	tmpdir="$(mktemp -d)"
	trap 'rm -rf "$tmpdir"' EXIT INT TERM

	binary="${tmpdir}/${asset}"
	installer="${tmpdir}/install-debian-binary.sh"

	echo "Installing ${APP_NAME} ${VERSION} for linux/${arch} from ${REPO}"
	download "${base}/${asset}" "$binary"
	chmod 0755 "$binary"

	download_installer "$VERSION" "$installer"
	chmod 0755 "$installer"

	sh "$installer" "$binary"
	print_access_hint
}

main "$@"
