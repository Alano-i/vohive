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
Environment=VOHIVE_VOWIFI_ENABLE_SWU=1
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
QMI_INTERFACE="4"
AT_INTERFACE="2"
BOOT_DISCOVERY_WAIT_SECONDS="15"
BOOT_REENUMERATION_WAIT_SECONDS="40"

is_dji_baiwang_usb() {
	usb="$1"
	[ -f "$usb/idVendor" ] || return 1
	[ -f "$usb/idProduct" ] || return 1
	[ "$(cat "$usb/idVendor")" = "$VID" ] || return 1
	[ "$(cat "$usb/idProduct")" = "$PID" ] || return 1
}

bind_drivers() {
	modprobe option || return 0
	modprobe qmi_wwan || return 0

	printf '%s %s' "$VID" "$PID" > /sys/bus/usb-serial/drivers/option1/new_id 2>/dev/null || true
	printf '%s %s' "$VID" "$PID" > /sys/bus/usb/drivers/qmi_wwan/new_id 2>/dev/null || true

	for usb in /sys/bus/usb/devices/*; do
		is_dji_baiwang_usb "$usb" || continue
		base="$(basename "$usb")"
		for intf in "$usb"/${base}:1.*; do
			[ -d "$intf" ] || continue
			interface_number="${intf##*.}"
			if [ "$interface_number" = "$QMI_INTERFACE" ]; then
				target_driver="qmi_wwan"
			else
				target_driver="option"
			fi
			if [ -w "$intf/driver_override" ]; then
				printf '%s' "$target_driver" > "$intf/driver_override" || true
			fi
			if [ -L "$intf/driver" ]; then
				drv="$(basename "$(readlink "$intf/driver")")"
				if [ "$drv" != "$target_driver" ]; then
					printf '%s' "$(basename "$intf")" > "$intf/driver/unbind" 2>/dev/null || true
				fi
			fi
			if [ ! -L "$intf/driver" ] && [ -w "/sys/bus/usb/drivers/$target_driver/bind" ]; then
				printf '%s' "$(basename "$intf")" > "/sys/bus/usb/drivers/$target_driver/bind" 2>/dev/null || true
			fi
		done
	done
}

count_devices() {
	count=0
	for usb in /sys/bus/usb/devices/*; do
		is_dji_baiwang_usb "$usb" || continue
		count=$((count + 1))
	done
	printf '%s\n' "$count"
}

device_signature() {
	for usb in /sys/bus/usb/devices/*; do
		is_dji_baiwang_usb "$usb" || continue
		[ -f "$usb/devnum" ] || continue
		printf '%s:%s ' "$(basename "$usb")" "$(cat "$usb/devnum")"
	done
}

count_reenumerated_devices() {
	before=" $1 "
	count=0
	for usb in /sys/bus/usb/devices/*; do
		is_dji_baiwang_usb "$usb" || continue
		[ -f "$usb/devnum" ] || continue
		signature="$(basename "$usb"):$(cat "$usb/devnum")"
		case "$before" in
			*" $signature "*) ;;
			*) count=$((count + 1)) ;;
		esac
	done
	printf '%s\n' "$count"
}

collect_at_ports() {
	for usb in /sys/bus/usb/devices/*; do
		is_dji_baiwang_usb "$usb" || continue
		base="$(basename "$usb")"
		for node in "$usb/${base}:1.${AT_INTERFACE}"/ttyUSB*/tty/ttyUSB*; do
			[ -e "$node" ] || continue
			printf '/dev/%s\n' "$(basename "$node")"
		done
	done
}

run_at_command() {
	port="$1"
	command="$2"
	timeout_seconds="${3:-3}"
	timeout "$timeout_seconds" sh -c '
		port="$1"
		command="$2"
		stty -F "$port" 115200 raw -echo -echoe -echok -echoctl -echoke 2>/dev/null || true
		exec 3<>"$port"
		printf "%s\r" "$command" >&3
		while IFS= read -r line <&3; do
			printf "%s\n" "$line"
			case "$line" in
				*OK*|*ERROR*) exit 0 ;;
			esac
		done
	' sh "$port" "$command" 2>/dev/null
}

count_ready_devices() {
	count=0
	for usb in /sys/bus/usb/devices/*; do
		is_dji_baiwang_usb "$usb" || continue
		base="$(basename "$usb")"
		qmi_intf="$usb/${base}:1.${QMI_INTERFACE}"
		at_intf="$usb/${base}:1.${AT_INTERFACE}"
		[ -L "$qmi_intf/driver" ] || continue
		[ "$(basename "$(readlink "$qmi_intf/driver")")" = "qmi_wwan" ] || continue
		at_ready=false
		for node in "$at_intf"/ttyUSB*/tty/ttyUSB*; do
			if [ -e "$node" ]; then
				at_ready=true
				break
			fi
		done
		[ "$at_ready" = true ] || continue
		count=$((count + 1))
	done
	printf '%s\n' "$count"
}

boot_reset_modems() {
	attempt=0
	expected=0
	ports=""
	while [ "$attempt" -lt "$BOOT_DISCOVERY_WAIT_SECONDS" ]; do
		bind_drivers
		expected="$(count_devices)"
		ports="$(collect_at_ports)"
		port_count="$(printf '%s\n' "$ports" | awk 'NF { count++ } END { print count + 0 }')"
		if [ "$expected" -gt 0 ] && [ "$port_count" -ge "$expected" ]; then
			break
		fi
		attempt=$((attempt + 1))
		sleep 1
	done

	if [ "$expected" -eq 0 ]; then
		echo "VoHive USB bootstrap: no DJI Baiwang modem detected; skipping boot reset"
		return 0
	fi
	if [ -z "$ports" ]; then
		echo "VoHive USB bootstrap: DJI Baiwang AT control ports not ready; continuing without reset" >&2
		return 0
	fi

	# USB serial nodes can appear before the modem firmware accepts AT commands,
	# especially during early host boot while the modem itself stayed powered.
	# Probe every dedicated interface 1.2 port and only issue CFUN after all
	# discovered modems have answered AT with OK.
	sleep 5
	attempt=0
	ready_at=0
	while [ "$attempt" -lt "$BOOT_DISCOVERY_WAIT_SECONDS" ]; do
		ready_at=0
		for port in $ports; do
			response="$(run_at_command "$port" AT 3 || true)"
			case "$response" in
				*OK*) ready_at=$((ready_at + 1)) ;;
			esac
		done
		if [ "$ready_at" -ge "$expected" ]; then
			break
		fi
		attempt=$((attempt + 1))
		sleep 1
	done
	if [ "$ready_at" -lt "$expected" ]; then
		echo "VoHive USB bootstrap: only $ready_at/$expected DJI Baiwang AT control ports became responsive" >&2
	fi

	before_signature="$(device_signature)"
	echo "VoHive USB bootstrap: resetting $expected externally-powered DJI Baiwang modem(s) before QMI startup"
	for port in $ports; do
		(run_at_command "$port" 'AT+CFUN=1,1' 5 >/dev/null || true) &
	done
	wait

	# A CFUN reset is asynchronous. Do not mistake the still-present old nodes
	# for a completed re-enumeration: every USB device must return with a new
	# kernel devnum before VoHive is allowed to open its QMI control endpoint.
	attempt=0
	reenumerated=0
	while [ "$attempt" -lt "$BOOT_REENUMERATION_WAIT_SECONDS" ]; do
		reenumerated="$(count_reenumerated_devices "$before_signature")"
		if [ "$reenumerated" -ge "$expected" ]; then
			break
		fi
		attempt=$((attempt + 1))
		sleep 1
	done
	if [ "$reenumerated" -lt "$expected" ]; then
		echo "VoHive USB bootstrap: only $reenumerated/$expected DJI Baiwang modem(s) completed a real USB re-enumeration" >&2
	fi

	# Re-assert the dedicated option/qmi_wwan bindings and wait until both the
	# AT control interface and QMI interface are ready for every returned modem.
	attempt=0
	while [ "$attempt" -lt "$BOOT_REENUMERATION_WAIT_SECONDS" ]; do
		bind_drivers
		ready="$(count_ready_devices)"
		if [ "$ready" -ge "$expected" ]; then
			echo "VoHive USB bootstrap: $ready DJI Baiwang modem(s) re-enumerated and ready"
			return 0
		fi
		attempt=$((attempt + 1))
		sleep 1
	done

	echo "VoHive USB bootstrap: timed out waiting for all DJI Baiwang modems; VoHive will continue with available devices" >&2
	return 0
}

bind_drivers
if [ "${1:-}" = "--boot-reset" ]; then
	boot_reset_modems
fi
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
ExecStart=${USB_BIND_SCRIPT} --boot-reset
RemainAfterExit=yes
TimeoutStartSec=90s

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
	systemctl stop "$APP_NAME" 2>/dev/null || true
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
