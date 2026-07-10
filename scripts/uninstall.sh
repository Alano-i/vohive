#!/bin/sh
set -eu

APP_NAME="vohive"
BIN_NAME="vohive"
INSTALL_BIN="/usr/local/bin/${BIN_NAME}"
CONFIG_DIR="/etc/${APP_NAME}"
WORK_DIR="/var/lib/${APP_NAME}"
SERVICE_FILE="/etc/systemd/system/${APP_NAME}.service"
SERVICE_DROPIN_DIR="/etc/systemd/system/${APP_NAME}.service.d"
USB_BIND_SCRIPT="/usr/local/sbin/${APP_NAME}-bind-dji-baiwang.sh"
USB_DRIVER_SERVICE="/etc/systemd/system/${APP_NAME}-usb-drivers.service"
USB_UDEV_RULE="/etc/udev/rules.d/99-${APP_NAME}-dji-baiwang.rules"

usage() {
	cat <<EOF
Usage:
  sudo sh scripts/uninstall.sh

This removes all VoHive-owned files:
  Binary:          ${INSTALL_BIN}
  Config dir:      ${CONFIG_DIR}
  Working dir:     ${WORK_DIR}
  systemd unit:    ${SERVICE_FILE}
  systemd drop-in: ${SERVICE_DROPIN_DIR}
  USB service:     ${USB_DRIVER_SERVICE}
  USB bind script: ${USB_BIND_SCRIPT}
  udev rule:       ${USB_UDEV_RULE}
EOF
}

die() {
	echo "error: $*" >&2
	exit 1
}

require_root() {
	if [ "$(id -u)" -ne 0 ]; then
		die "please run as root, for example: sudo sh scripts/uninstall.sh"
	fi
}

has_systemctl() {
	command -v systemctl >/dev/null 2>&1
}

systemctl_quiet() {
	if has_systemctl; then
		systemctl "$@" >/dev/null 2>&1 || true
	fi
}

stop_services() {
	systemctl_quiet stop "${APP_NAME}.service"
	systemctl_quiet stop "${APP_NAME}-usb-drivers.service"
	systemctl_quiet disable "${APP_NAME}.service"
	systemctl_quiet disable "${APP_NAME}-usb-drivers.service"
	systemctl_quiet reset-failed "${APP_NAME}.service"
	systemctl_quiet reset-failed "${APP_NAME}-usb-drivers.service"
}

remove_paths() {
	rm -f "$INSTALL_BIN"
	rm -rf "$CONFIG_DIR"
	rm -rf "$WORK_DIR"
	rm -f "$SERVICE_FILE"
	rm -rf "$SERVICE_DROPIN_DIR"
	rm -f "$USB_DRIVER_SERVICE"
	rm -f "$USB_BIND_SCRIPT"
	rm -f "$USB_UDEV_RULE"
}

reload_system() {
	systemctl_quiet daemon-reload
	if command -v udevadm >/dev/null 2>&1; then
		udevadm control --reload-rules >/dev/null 2>&1 || true
		udevadm trigger >/dev/null 2>&1 || true
	fi
}

main() {
	if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
		usage
		exit 0
	fi
	[ $# -eq 0 ] || die "unknown argument: $1"

	require_root
	stop_services
	remove_paths
	reload_system

	echo "VoHive uninstalled."
}

main "$@"
