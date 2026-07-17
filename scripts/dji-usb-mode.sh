#!/bin/sh
set -eu

MODE="${DJI_USB_MODE:-}"

SYSFS_ROOT="${DJI_USB_SYSFS_ROOT:-/sys}"
DEV_ROOT="${DJI_USB_DEV_ROOT:-/dev}"
BACKUP_DIR="${DJI_USB_BACKUP_DIR:-/var/lib/vohive/dji-usb-backups}"
LOCK_DIR="${DJI_USB_LOCK_DIR:-/run/vohive-dji-usb-mode.lock}"
AT_RUNNER="${DJI_USB_AT_RUNNER:-}"
SKIP_DRIVER_BIND="${DJI_USB_SKIP_DRIVER_BIND:-0}"
SKIP_SERVICES="${DJI_USB_SKIP_SERVICES:-0}"
ALLOW_NON_ROOT="${DJI_USB_ALLOW_NON_ROOT:-0}"
AT_WAIT_SECONDS="${DJI_USB_AT_WAIT_SECONDS:-20}"
REENUMERATE_WAIT_SECONDS="${DJI_USB_REENUMERATE_WAIT_SECONDS:-25}"
DRY_RUN=0

USB_DEVICES_DIR="${SYSFS_ROOT}/bus/usb/devices"
OPTION_DRIVER_DIR="${SYSFS_ROOT}/bus/usb-serial/drivers/option1"
USB_DRIVER_DIR="${SYSFS_ROOT}/bus/usb/drivers/usb"
MODULES_DIR="${SYSFS_ROOT}/module"

LOCK_HELD=0
STOPPED_SERVICES=""
OPTION_WAS_LOADED=0
DYNAMIC_ID_REGISTERED=0

usage() {
	cat <<EOF
Usage:
  sudo sh scripts/dji-usb-mode.sh [--dry-run]

After startup, select one operation:
  1. Convert every connected DJI Baiwang module from 2ca3:4006 to
     the native Quectel EC25 USB ID 2c7c:0125.
  2. Restore every connected spoofed Baiwang module from 2c7c:0125
     to the DJI USB ID 2ca3:4006.

Each module is identified by IMEI. The original USBCFG is saved under:
  ${BACKUP_DIR}
EOF
}

die() {
	echo "error: $*" >&2
	exit 1
}

log() {
	printf '%s\n' "$*"
}

while [ $# -gt 0 ]; do
	case "$1" in
		--dry-run)
			DRY_RUN=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown argument: $1"
			;;
	esac
done

choose_mode() {
	if [ -n "$MODE" ]; then
		return 0
	fi
	cat <<'EOF'
请选择操作：
  1) 将所有 DJI Baiwang 模块伪装为 2c7c:0125
  2) 将所有已伪装的 Baiwang 模块恢复为 2ca3:4006
  0) 退出
EOF
	printf '请输入选项 [0-2]: '
	IFS= read -r choice || die "unable to read selection"
	case "$choice" in
		1)
			MODE="spoof"
			;;
		2)
			MODE="restore"
			;;
		0)
			log "已取消。"
			exit 0
			;;
		*)
			die "invalid selection: $choice"
			;;
	esac
}

choose_mode

case "$MODE" in
	spoof)
		SOURCE_VID="2ca3"
		SOURCE_PID="4006"
		TARGET_VID="2c7c"
		TARGET_PID="0125"
		ACTION_TEXT="spoof as Quectel EC25"
		;;
	restore)
		SOURCE_VID="2c7c"
		SOURCE_PID="0125"
		TARGET_VID="2ca3"
		TARGET_PID="4006"
		ACTION_TEXT="restore as DJI Baiwang"
		;;
	*)
		die "internal mode must be spoof or restore"
		;;
esac

require_root() {
	if [ "$ALLOW_NON_ROOT" != "1" ] && [ "$(id -u)" -ne 0 ]; then
		die "please run as root"
	fi
}

require_commands() {
	for command in awk grep sed tr head mkdir mv sleep; do
		command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
	done
	if [ -z "$AT_RUNNER" ]; then
		command -v python3 >/dev/null 2>&1 || die "python3 is required for safe serial-port access"
	fi
	if [ "$SKIP_DRIVER_BIND" != "1" ]; then
		command -v modprobe >/dev/null 2>&1 || die "modprobe is required"
	fi
}

resume_services() {
	[ "$SKIP_SERVICES" = "1" ] && return 0
	command -v systemctl >/dev/null 2>&1 || return 0
	for service in $STOPPED_SERVICES; do
		systemctl start "$service" >/dev/null 2>&1 || true
	done
}

cleanup_driver_state() {
	[ "$SKIP_DRIVER_BIND" = "1" ] && return 0
	if [ "$DYNAMIC_ID_REGISTERED" -eq 1 ] && [ -w "$OPTION_DRIVER_DIR/remove_id" ]; then
		printf '2ca3 4006' > "$OPTION_DRIVER_DIR/remove_id" 2>/dev/null || true
	fi
	if [ "$DRY_RUN" -eq 1 ] && [ "$OPTION_WAS_LOADED" -eq 0 ]; then
		modprobe -r option >/dev/null 2>&1 || true
	fi
}

cleanup() {
	status=$?
	trap - EXIT INT TERM
	cleanup_driver_state
	resume_services
	if [ "$LOCK_HELD" -eq 1 ]; then
		rmdir "$LOCK_DIR" >/dev/null 2>&1 || true
	fi
	exit "$status"
}

trap cleanup EXIT INT TERM

acquire_lock() {
	parent="$(dirname "$LOCK_DIR")"
	mkdir -p "$parent"
	if ! mkdir "$LOCK_DIR" 2>/dev/null; then
		die "another DJI USB mode operation is already running: $LOCK_DIR"
	fi
	LOCK_HELD=1
}

pause_service_if_active() {
	service="$1"
	[ "$SKIP_SERVICES" = "1" ] && return 0
	command -v systemctl >/dev/null 2>&1 || return 0
	if systemctl is-active --quiet "$service" 2>/dev/null; then
		log "Stopping $service while modem USB IDs are changed"
		systemctl stop "$service"
		STOPPED_SERVICES="$service $STOPPED_SERVICES"
	fi
}

read_file() {
	file="$1"
	[ -f "$file" ] || return 1
	tr -d '[:space:]' < "$file"
}

usb_matches() {
	usb="$1"
	vid="$2"
	pid="$3"
	[ "$(read_file "$usb/idVendor" 2>/dev/null || true)" = "$vid" ] &&
		[ "$(read_file "$usb/idProduct" 2>/dev/null || true)" = "$pid" ]
}

is_baiwang_identity() {
	usb="$1"
	manufacturer="$(cat "$usb/manufacturer" 2>/dev/null || true)"
	product="$(cat "$usb/product" 2>/dev/null || true)"
	printf '%s\n%s\n' "$manufacturer" "$product" | grep -Eiq 'baiwang|dji'
}

is_candidate_device() {
	usb="$1"
	vid="$2"
	pid="$3"
	usb_matches "$usb" "$vid" "$pid" || return 1
	if [ "$vid:$pid" = "2c7c:0125" ]; then
		is_baiwang_identity "$usb" || return 1
	fi
}

list_devices() {
	vid="$1"
	pid="$2"
	for usb in "$USB_DEVICES_DIR"/*; do
		[ -d "$usb" ] || continue
		is_candidate_device "$usb" "$vid" "$pid" || continue
		basename "$usb"
	done
}

count_devices() {
	count=0
	for unused in $(list_devices "$1" "$2"); do
		count=$((count + 1))
	done
	printf '%s\n' "$count"
}

register_original_driver_id() {
	[ "$SKIP_DRIVER_BIND" = "1" ] && return 0
	modprobe option
	if [ -w "$OPTION_DRIVER_DIR/new_id" ]; then
		if printf '2ca3 4006' > "$OPTION_DRIVER_DIR/new_id" 2>/dev/null; then
			DYNAMIC_ID_REGISTERED=1
		fi
	fi
	if command -v udevadm >/dev/null 2>&1; then
		udevadm settle >/dev/null 2>&1 || true
	fi
}

prepare_drivers() {
	[ "$SKIP_DRIVER_BIND" = "1" ] && return 0
	modprobe option
	if [ "$MODE" = "spoof" ] || [ "$(count_devices 2ca3 4006)" -gt 0 ]; then
		register_original_driver_id
	fi
}

port_is_usable() {
	port="$1"
	if [ -n "$AT_RUNNER" ]; then
		[ -e "$port" ]
	else
		[ -c "$port" ]
	fi
}

find_at_port() {
	usb_name="$1"
	usb="$USB_DEVICES_DIR/$usb_name"
	interface="$usb/${usb_name}:1.2"
	for node in "$interface"/ttyUSB*/tty/ttyUSB* "$interface"/ttyUSB*; do
		[ -e "$node" ] || continue
		port="$DEV_ROOT/$(basename "$node")"
		port_is_usable "$port" || continue
		printf '%s\n' "$port"
		return 0
	done
	return 1
}

wait_for_at_port() {
	usb_name="$1"
	attempt=0
	while [ "$attempt" -lt "$AT_WAIT_SECONDS" ]; do
		port="$(find_at_port "$usb_name" 2>/dev/null || true)"
		if [ -n "$port" ]; then
			printf '%s\n' "$port"
			return 0
		fi
		attempt=$((attempt + 1))
		sleep 1
	done
	return 1
}

run_at() {
	port="$1"
	command_text="$2"
	timeout_seconds="${3:-5}"
	if [ -n "$AT_RUNNER" ]; then
		"$AT_RUNNER" "$port" "$command_text" "$timeout_seconds"
		return
	fi
	python3 - "$port" "$command_text" "$timeout_seconds" <<'PY'
import os
import select
import sys
import termios
import time

port, command, timeout_text = sys.argv[1:4]
timeout = float(timeout_text)

# Deliberately omit O_CREAT. If a modem disappears during a reset, this fails
# instead of replacing /dev/ttyUSBX with a regular file.
fd = os.open(port, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
try:
    attrs = termios.tcgetattr(fd)
    attrs[0] = 0
    attrs[1] = 0
    attrs[2] = termios.B115200 | termios.CS8 | termios.CLOCAL | termios.CREAD
    attrs[3] = 0
    attrs[4] = termios.B115200
    attrs[5] = termios.B115200
    attrs[6][termios.VMIN] = 0
    attrs[6][termios.VTIME] = 0
    termios.tcsetattr(fd, termios.TCSANOW, attrs)
    termios.tcflush(fd, termios.TCIFLUSH)
    os.write(fd, (command + "\r").encode("ascii"))

    data = bytearray()
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        ready, _, _ = select.select([fd], [], [], 0.2)
        if not ready:
            continue
        try:
            chunk = os.read(fd, 4096)
        except BlockingIOError:
            continue
        if not chunk:
            continue
        data.extend(chunk)
        text = data.decode("utf-8", "replace")
        if "\r\nOK\r\n" in text or "\r\nERROR\r\n" in text or "+CME ERROR:" in text:
            break

    sys.stdout.write(data.decode("utf-8", "replace"))
finally:
    os.close(fd)
PY
}

response_ok() {
	printf '%s\n' "$1" | tr -d '\r' | grep -q '^OK$'
}

extract_imei() {
	printf '%s\n' "$1" | tr -d '\r' | grep -Eo '[0-9]{15}' | head -n 1
}

extract_usbcfg() {
	printf '%s\n' "$1" | tr -d '\r' | sed -n 's/^+QCFG:[[:space:]]*//Ip' | head -n 1
}

cfg_has_ids() {
	cfg="$1"
	vid="$2"
	pid="$3"
	case "$vid:$pid" in
		2ca3:4006)
			printf '%s\n' "$cfg" | grep -Eq '0[xX]2[cC][aA]3[[:space:]]*,[[:space:]]*0[xX]4006'
			;;
		2c7c:0125)
			printf '%s\n' "$cfg" | grep -Eq '0[xX]2[cC]7[cC][[:space:]]*,[[:space:]]*0[xX]0*125'
			;;
		*)
			return 1
			;;
	esac
}

replace_cfg_ids() {
	cfg="$1"
	if [ "$MODE" = "spoof" ]; then
		printf '%s\n' "$cfg" | sed -E 's/0[xX]2[cC][aA]3[[:space:]]*,[[:space:]]*0[xX]4006/0x2C7C,0x0125/'
	else
		printf '%s\n' "$cfg" | sed -E 's/0[xX]2[cC]7[cC][[:space:]]*,[[:space:]]*0[xX]0*125/0x2CA3,0x4006/'
	fi
}

normalize_cfg() {
	printf '%s\n' "$1" |
		tr '[:upper:]' '[:lower:]' |
		tr -d '[:space:]' |
		sed -E 's/0x0*([0-9a-f]+)/0x\1/g'
}

cfg_equal() {
	[ "$(normalize_cfg "$1")" = "$(normalize_cfg "$2")" ]
}

save_original_config() {
	imei="$1"
	cfg="$2"
	mkdir -p "$BACKUP_DIR"
	chmod 0700 "$BACKUP_DIR" 2>/dev/null || true
	backup="$BACKUP_DIR/${imei}.usbcfg"
	if [ -f "$backup" ]; then
		stored="$(head -n 1 "$backup")"
		[ "$stored" = "$cfg" ] || die "stored USBCFG for IMEI $imei differs from the connected module"
		return 0
	fi
	tmp="$backup.tmp.$$"
	umask 077
	printf '%s\n' "$cfg" > "$tmp"
	mv "$tmp" "$backup"
}

wait_for_usb_id() {
	usb_name="$1"
	vid="$2"
	pid="$3"
	attempt=0
	while [ "$attempt" -lt "$REENUMERATE_WAIT_SECONDS" ]; do
		if usb_matches "$USB_DEVICES_DIR/$usb_name" "$vid" "$pid"; then
			return 0
		fi
		attempt=$((attempt + 1))
		sleep 1
	done
	return 1
}

host_rebind_usb() {
	usb_name="$1"
	[ "$SKIP_DRIVER_BIND" = "1" ] && return 1
	[ -w "$USB_DRIVER_DIR/unbind" ] || return 1
	[ -w "$USB_DRIVER_DIR/bind" ] || return 1
	log "  USB ID has not refreshed; re-binding physical USB path $usb_name"
	printf '%s' "$usb_name" > "$USB_DRIVER_DIR/unbind" 2>/dev/null || return 1
	sleep 2
	printf '%s' "$usb_name" > "$USB_DRIVER_DIR/bind" 2>/dev/null || return 1
}

verify_target() {
	usb_name="$1"
	expected_cfg="$2"
	if [ "$TARGET_VID:$TARGET_PID" = "2ca3:4006" ]; then
		register_original_driver_id
	fi
	attempt=0
	last_cfg=""
	while [ "$attempt" -lt "$AT_WAIT_SECONDS" ]; do
		port="$(find_at_port "$usb_name" 2>/dev/null || true)"
		if [ -n "$port" ]; then
			probe="$(run_at "$port" AT 3 || true)"
			if response_ok "$probe"; then
				response="$(run_at "$port" 'AT+QCFG="USBCFG"?' 5 || true)"
				last_cfg="$(extract_usbcfg "$response" || true)"
				if [ -n "$last_cfg" ] && cfg_equal "$last_cfg" "$expected_cfg"; then
					return 0
				fi
			fi
		fi
		attempt=$((attempt + 1))
		sleep 1
	done
	if [ -n "$last_cfg" ]; then
		die "USBCFG verification failed for $usb_name: $last_cfg"
	fi
	die "target USB ID appeared for $usb_name, but its AT interface did not become ready"
}

process_device() {
	usb_name="$1"
	index="$2"
	total="$3"

	port="$(wait_for_at_port "$usb_name" || true)"
	[ -n "$port" ] || die "AT interface 2 did not become ready for USB path $usb_name"

	probe="$(run_at "$port" AT 3 || true)"
	response_ok "$probe" || die "AT probe failed for $usb_name on $port"

	imei_response="$(run_at "$port" 'AT+CGSN' 5 || true)"
	imei="$(extract_imei "$imei_response" || true)"
	[ -n "$imei" ] || die "unable to read IMEI from $usb_name on $port"

	cfg_response="$(run_at "$port" 'AT+QCFG="USBCFG"?' 5 || true)"
	current_cfg="$(extract_usbcfg "$cfg_response" || true)"
	[ -n "$current_cfg" ] || die "unable to read USBCFG from IMEI $imei"
	cfg_has_ids "$current_cfg" "$SOURCE_VID" "$SOURCE_PID" ||
		die "IMEI $imei returned an unexpected USBCFG: $current_cfg"

	target_cfg="$(replace_cfg_ids "$current_cfg")"
	cfg_has_ids "$target_cfg" "$TARGET_VID" "$TARGET_PID" ||
		die "unable to build target USBCFG for IMEI $imei"

	if [ "$MODE" = "spoof" ] && [ "$DRY_RUN" -eq 0 ]; then
		save_original_config "$imei" "$current_cfg"
	fi

	log "[$index/$total] IMEI $imei, USB $usb_name, AT $port"
	log "  current: $current_cfg"
	log "  target:  $target_cfg"

	if [ "$DRY_RUN" -eq 1 ]; then
		log "  dry-run: no command was written"
		return 0
	fi

	set_response="$(run_at "$port" "AT+QCFG=$target_cfg" 10 || true)"
	response_ok "$set_response" || die "module IMEI $imei rejected the target USBCFG: $set_response"
	log "  USBCFG accepted; restarting the module"

	# A reset may close the port before a response is returned. The QCFG write
	# above has already been acknowledged, so this command is best-effort.
	run_at "$port" 'AT+CFUN=1,1' 5 >/dev/null 2>&1 || true

	if ! wait_for_usb_id "$usb_name" "$TARGET_VID" "$TARGET_PID"; then
		host_rebind_usb "$usb_name" || true
		wait_for_usb_id "$usb_name" "$TARGET_VID" "$TARGET_PID" ||
			die "IMEI $imei did not re-enumerate as $TARGET_VID:$TARGET_PID"
	fi

	verify_target "$usb_name" "$target_cfg"
	log "  verified: $TARGET_VID:$TARGET_PID"
}

main() {
	require_root
	require_commands
	[ -d "$USB_DEVICES_DIR" ] || die "USB sysfs directory not found: $USB_DEVICES_DIR"
	if [ -d "$MODULES_DIR/option" ]; then
		OPTION_WAS_LOADED=1
	fi
	acquire_lock
	pause_service_if_active vohive.service
	pause_service_if_active ModemManager.service
	prepare_drivers

	total="$(count_devices "$SOURCE_VID" "$SOURCE_PID")"
	already="$(count_devices "$TARGET_VID" "$TARGET_PID")"
	if [ "$total" -eq 0 ]; then
		if [ "$already" -gt 0 ]; then
			log "All $already connected module(s) already use $TARGET_VID:$TARGET_PID."
			exit 0
		fi
		die "no connected module found with USB ID $SOURCE_VID:$SOURCE_PID"
	fi

	log "Found $total module(s) to $ACTION_TEXT."
	[ "$already" -eq 0 ] || log "$already module(s) already use $TARGET_VID:$TARGET_PID and will be skipped."
	[ "$DRY_RUN" -eq 0 ] || log "Dry-run mode is enabled."

	index=0
	for usb_name in $(list_devices "$SOURCE_VID" "$SOURCE_PID"); do
		index=$((index + 1))
		process_device "$usb_name" "$index" "$total"
	done

	if [ "$DRY_RUN" -eq 1 ]; then
		log "Dry-run completed for $total module(s)."
	else
		remaining="$(count_devices "$SOURCE_VID" "$SOURCE_PID")"
		[ "$remaining" -eq 0 ] || die "$remaining module(s) still use $SOURCE_VID:$SOURCE_PID"
		log "Completed. $total module(s) now use $TARGET_VID:$TARGET_PID."
	fi
}

main
