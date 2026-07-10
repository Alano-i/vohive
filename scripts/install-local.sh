#!/bin/sh
set -eu

APP_NAME="vohive"
BIN_NAME="vohive"
INSTALL_BIN="/usr/local/bin/${BIN_NAME}"
CONFIG_DIR="/etc/${APP_NAME}"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
WORK_DIR="/var/lib/${APP_NAME}"
SERVICE_FILE="/etc/systemd/system/${APP_NAME}.service"
USB_BIND_SCRIPT="/usr/local/sbin/${APP_NAME}-bind-dji-baiwang.sh"
USB_DRIVER_SERVICE="/etc/systemd/system/${APP_NAME}-usb-drivers.service"
USB_UDEV_RULE="/etc/udev/rules.d/99-${APP_NAME}-dji-baiwang.rules"

usage() {
	cat <<EOF
Usage:
  sudo sh scripts/install-local.sh [path-to-vohive-binary]

If no binary path is provided, the script auto-detects one from:
  ./dist/vohive_*_linux_<arch>

Installed paths:
  Binary:        ${INSTALL_BIN}
  Config file:   ${CONFIG_FILE}
  Working dir:   ${WORK_DIR}
  Data dir:      ${WORK_DIR}/data
  Log dir:       ${WORK_DIR}/logs
  systemd unit:  ${SERVICE_FILE}
EOF
}

die() {
	echo "error: $*" >&2
	exit 1
}

require_root() {
	if [ "$(id -u)" -ne 0 ]; then
		die "please run as root, for example: sudo sh scripts/install-local.sh"
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

find_binary() {
	if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
		usage
		exit 0
	fi

	if [ -n "${1:-}" ]; then
		[ -f "$1" ] || die "binary not found: $1"
		echo "$1"
		return
	fi

	arch="$(detect_arch)"
	candidate="$(ls -t ./dist/${APP_NAME}_*_linux_${arch} 2>/dev/null | head -n 1 || true)"
	[ -n "$candidate" ] || die "no binary found for linux/${arch}; run make build-${arch} first or pass a binary path"
	echo "$candidate"
}

install_runtime_packages() {
	if command -v apt-get >/dev/null 2>&1; then
		apt-get update
		DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates tzdata kmod udev usbutils
	fi
}

install_binary() {
	src="$1"
	install -m 0755 "$src" "$INSTALL_BIN"
}

write_default_config() {
	mkdir -p "$CONFIG_DIR"
	if [ -f "$CONFIG_FILE" ]; then
		echo "config exists, keep unchanged: $CONFIG_FILE"
		return
	fi

	cat > "$CONFIG_FILE" <<'EOF'
server:
  debug: false
  port: 7575

web:
  username: admin
  password: admin

devices: []

vowifi:
  enabled: false

webhook:
  enabled: false
EOF
	chmod 0640 "$CONFIG_FILE"
}

write_service() {
	cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=VoHive modem management service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${WORK_DIR}
ExecStart=${INSTALL_BIN} -c ${CONFIG_FILE}
Restart=always
RestartSec=5s
Environment=CONFIG_PATH=${CONFIG_FILE}
Environment=HOME=${WORK_DIR}
LimitCORE=0

[Install]
WantedBy=multi-user.target
EOF

	mkdir -p "/etc/systemd/system/${APP_NAME}.service.d"
	cat > "/etc/systemd/system/${APP_NAME}.service.d/10-usb-drivers.conf" <<EOF
[Unit]
Requires=${APP_NAME}-usb-drivers.service
After=${APP_NAME}-usb-drivers.service
EOF
}

write_usb_driver_binding() {
	cat > "$USB_BIND_SCRIPT" <<'EOF'
#!/bin/sh
set -eu

VID="2ca3"
PID="4006"

modprobe option || exit 0

for usb in /sys/bus/usb/devices/*; do
	[ -f "$usb/idVendor" ] || continue
	[ -f "$usb/idProduct" ] || continue
	[ "$(cat "$usb/idVendor")" = "$VID" ] || continue
	[ "$(cat "$usb/idProduct")" = "$PID" ] || continue

	base="$(basename "$usb")"
	for intf in "$usb"/${base}:1.*; do
		[ -d "$intf" ] || continue
		if [ -w "$intf/driver_override" ]; then
			printf 'option' > "$intf/driver_override" || true
		fi
		if [ -L "$intf/driver" ]; then
			drv="$(basename "$(readlink "$intf/driver")")"
			if [ "$drv" != "option" ]; then
				printf '%s' "$(basename "$intf")" > "$intf/driver/unbind" 2>/dev/null || true
			fi
		fi
	done

	printf '%s %s' "$VID" "$PID" > /sys/bus/usb-serial/drivers/option1/new_id 2>/dev/null || true

	for intf in "$usb"/${base}:1.*; do
		[ -d "$intf" ] || continue
		[ -L "$intf/driver" ] && continue
		printf '%s' "$(basename "$intf")" > /sys/bus/usb/drivers_probe 2>/dev/null || true
	done
done
EOF
	chmod 0755 "$USB_BIND_SCRIPT"

	cat > "$USB_DRIVER_SERVICE" <<EOF
[Unit]
Description=VoHive extra USB modem driver bindings
DefaultDependencies=no
Before=${APP_NAME}.service
After=systemd-modules-load.service

[Service]
Type=oneshot
ExecStart=${USB_BIND_SCRIPT}
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

	cat > "$USB_UDEV_RULE" <<EOF
ACTION=="add", SUBSYSTEM=="usb", ATTR{idVendor}=="2ca3", ATTR{idProduct}=="4006", RUN+="${USB_BIND_SCRIPT}"
EOF
}

main() {
	if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
		usage
		exit 0
	fi

	require_root
	binary_path="$(find_binary "${1:-}")"

	if [ -f /etc/debian_version ]; then
		echo "detected Debian-compatible system"
	else
		echo "warning: /etc/debian_version not found; continuing anyway"
	fi

	install_runtime_packages
	mkdir -p "${WORK_DIR}/data" "${WORK_DIR}/logs"
	install_binary "$binary_path"
	write_default_config
	write_usb_driver_binding
	write_service

	systemctl daemon-reload
	udevadm control --reload-rules || true
	systemctl enable "${APP_NAME}-usb-drivers.service"
	systemctl restart "${APP_NAME}-usb-drivers.service" || true
	systemctl enable "$APP_NAME"
	systemctl restart "$APP_NAME"

	echo
	echo "VoHive installed."
	echo "Binary:      ${INSTALL_BIN}"
	echo "Config file: ${CONFIG_FILE}"
	echo "Work dir:    ${WORK_DIR}"
	echo
	echo "Useful commands:"
	echo "  systemctl status ${APP_NAME}"
	echo "  journalctl -u ${APP_NAME} -f"
}

main "$@"
