#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/vohive-dji-usb-test.XXXXXX")
SYSFS_ROOT="$TMP_DIR/sys"
DEV_ROOT="$TMP_DIR/dev"
STATE_DIR="$TMP_DIR/state"
BACKUP_DIR="$TMP_DIR/backups"
LOCK_DIR="$TMP_DIR/lock"
RUNNER="$TMP_DIR/mock-at-runner.sh"

cleanup() {
	rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

assert_file() {
	file="$1"
	expected="$2"
	actual="$(cat "$file")"
	[ "$actual" = "$expected" ] || fail "$file: got '$actual', want '$expected'"
}

mkdir -p "$SYSFS_ROOT/bus/usb/devices" "$DEV_ROOT" "$STATE_DIR"

cat > "$RUNNER" <<'EOF'
#!/bin/sh
set -eu

port="$1"
command_text="$2"
name="$(basename "$port")"
imei="$(cat "$MOCK_STATE_DIR/$name.imei")"
usb_name="$(cat "$MOCK_STATE_DIR/$name.usb")"
cfg_file="$MOCK_STATE_DIR/$name.cfg"
usb_dir="$MOCK_SYSFS_ROOT/bus/usb/devices/$usb_name"

case "$command_text" in
	AT)
		printf 'AT\r\n\r\nOK\r\n'
		;;
	AT+CGSN)
		printf 'AT+CGSN\r\n%s\r\n\r\nOK\r\n' "$imei"
		;;
	'AT+QCFG="USBCFG"?')
		printf 'AT+QCFG="USBCFG"?\r\n+QCFG: %s\r\n\r\nOK\r\n' "$(cat "$cfg_file")"
		;;
	AT+QCFG=*)
		cfg="${command_text#AT+QCFG=}"
		printf '%s\n' "$cfg" > "$cfg_file"
		case "$cfg" in
			*0x2C7C,0x0125*)
				cfg="$(printf '%s\n' "$cfg" | sed 's/0x0125/0x125/')"
				printf '%s\n' "$cfg" > "$cfg_file"
				printf '2c7c\n' > "$usb_dir/idVendor"
				printf '0125\n' > "$usb_dir/idProduct"
				;;
			*0x2CA3,0x4006*)
				printf '2ca3\n' > "$usb_dir/idVendor"
				printf '4006\n' > "$usb_dir/idProduct"
				;;
			*)
				printf 'ERROR\r\n'
				exit 0
				;;
		esac
		printf 'AT+QCFG=%s\r\n\r\nOK\r\n' "$cfg"
		;;
	AT+CFUN=1,1)
		printf 'AT+CFUN=1,1\r\n\r\nOK\r\n'
		;;
	*)
		printf 'ERROR\r\n'
		;;
esac
EOF
chmod 0755 "$RUNNER"

add_module() {
	usb_name="$1"
	port_name="$2"
	imei="$3"
	cfg="$4"
	usb="$SYSFS_ROOT/bus/usb/devices/$usb_name"
	mkdir -p "$usb/${usb_name}:1.2/$port_name/tty/$port_name"
	printf '2ca3\n' > "$usb/idVendor"
	printf '4006\n' > "$usb/idProduct"
	printf 'BAIWANG\n' > "$usb/manufacturer"
	printf 'Baiwang\n' > "$usb/product"
	: > "$DEV_ROOT/$port_name"
	printf '%s\n' "$imei" > "$STATE_DIR/$port_name.imei"
	printf '%s\n' "$usb_name" > "$STATE_DIR/$port_name.usb"
	printf '%s\n' "$cfg" > "$STATE_DIR/$port_name.cfg"
}

CFG_A='"usbcfg",0x2CA3,0x4006,1,1,1,1,1,0,0'
CFG_B='"usbcfg",0x2CA3,0x4006,1,0,1,1,1,1,0'
TARGET_A='"usbcfg",0x2C7C,0x125,1,1,1,1,1,0,0'
TARGET_B='"usbcfg",0x2C7C,0x125,1,0,1,1,1,1,0'

add_module 3-1 ttyUSB2 866069053316632 "$CFG_A"
add_module 5-2 ttyUSB7 866069053163836 "$CFG_B"

MENU_OUTPUT="$(printf '0\n' | sh "$SCRIPT_DIR/dji-usb-mode.sh")"
printf '%s\n' "$MENU_OUTPUT" | grep -q '请选择操作' || fail "interactive menu was not displayed"
printf '%s\n' "$MENU_OUTPUT" | grep -q '已取消' || fail "interactive cancel did not exit cleanly"

run_mode() {
	mode="$1"
	shift
	MOCK_SYSFS_ROOT="$SYSFS_ROOT" \
	MOCK_STATE_DIR="$STATE_DIR" \
	DJI_USB_SYSFS_ROOT="$SYSFS_ROOT" \
	DJI_USB_DEV_ROOT="$DEV_ROOT" \
	DJI_USB_BACKUP_DIR="$BACKUP_DIR" \
	DJI_USB_LOCK_DIR="$LOCK_DIR" \
	DJI_USB_AT_RUNNER="$RUNNER" \
	DJI_USB_SKIP_DRIVER_BIND=1 \
	DJI_USB_SKIP_SERVICES=1 \
	DJI_USB_ALLOW_NON_ROOT=1 \
	DJI_USB_AT_WAIT_SECONDS=1 \
	DJI_USB_REENUMERATE_WAIT_SECONDS=1 \
	DJI_USB_MODE="$mode" \
		sh "$SCRIPT_DIR/dji-usb-mode.sh" "$@"
}

run_mode spoof --dry-run
assert_file "$SYSFS_ROOT/bus/usb/devices/3-1/idVendor" 2ca3
assert_file "$SYSFS_ROOT/bus/usb/devices/5-2/idVendor" 2ca3
[ ! -d "$BACKUP_DIR" ] || fail "dry-run unexpectedly created the backup directory"

run_mode spoof

assert_file "$SYSFS_ROOT/bus/usb/devices/3-1/idVendor" 2c7c
assert_file "$SYSFS_ROOT/bus/usb/devices/3-1/idProduct" 0125
assert_file "$SYSFS_ROOT/bus/usb/devices/5-2/idVendor" 2c7c
assert_file "$SYSFS_ROOT/bus/usb/devices/5-2/idProduct" 0125
assert_file "$STATE_DIR/ttyUSB2.cfg" "$TARGET_A"
assert_file "$STATE_DIR/ttyUSB7.cfg" "$TARGET_B"
assert_file "$BACKUP_DIR/866069053316632.usbcfg" "$CFG_A"
assert_file "$BACKUP_DIR/866069053163836.usbcfg" "$CFG_B"

# A genuine EC25 uses the same 2c7c:0125 ID but must never be restored as DJI.
GENUINE="$SYSFS_ROOT/bus/usb/devices/7-1"
mkdir -p "$GENUINE"
printf '2c7c\n' > "$GENUINE/idVendor"
printf '0125\n' > "$GENUINE/idProduct"
printf 'Quectel\n' > "$GENUINE/manufacturer"
printf 'EC25\n' > "$GENUINE/product"

run_mode restore

assert_file "$SYSFS_ROOT/bus/usb/devices/3-1/idVendor" 2ca3
assert_file "$SYSFS_ROOT/bus/usb/devices/3-1/idProduct" 4006
assert_file "$SYSFS_ROOT/bus/usb/devices/5-2/idVendor" 2ca3
assert_file "$SYSFS_ROOT/bus/usb/devices/5-2/idProduct" 4006
assert_file "$STATE_DIR/ttyUSB2.cfg" "$CFG_A"
assert_file "$STATE_DIR/ttyUSB7.cfg" "$CFG_B"
assert_file "$GENUINE/idVendor" 2c7c
assert_file "$GENUINE/idProduct" 0125

echo "PASS: DJI USB spoof and restore simulation"
