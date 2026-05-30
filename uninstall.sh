#!/bin/bash
set -e

# Run as root on Raspberry Pi OS Buster to remove displaycameras.

if [[ $EUID -ne 0 ]]; then
	echo "This uninstaller must be run as root."
	exit 1
fi

CONFIG_DIR=/etc/displaycameras
SERVICE_FILE=/etc/systemd/system/displaycameras.service
SHARE_DIR=/usr/local/share/displaycameras
GO_ROOT=/usr/local/lib/displaycameras-go
BINARY=/usr/bin/displaycameras

echo "Stopping displaycameras service."
systemctl stop displaycameras 2>/dev/null || true

echo "Disabling displaycameras service."
systemctl disable displaycameras 2>/dev/null || true

if [ -e "$SERVICE_FILE" ]; then
	echo "Removing systemd service."
	rm -f "$SERVICE_FILE"
fi

if [ -e "$BINARY" ]; then
	echo "Removing displaycameras binary."
	rm -f "$BINARY"
fi

if [ -d "$SHARE_DIR" ]; then
	echo "Removing shared displaycameras files."
	rm -rf "$SHARE_DIR"
fi

if [ -d "$GO_ROOT" ]; then
	echo "Removing displaycameras Go toolchain."
	rm -rf "$GO_ROOT"
fi

if [ -e /etc/cron.d/repaircameras ]; then
	echo "Removing legacy repair cron job."
	rm -f /etc/cron.d/repaircameras
	systemctl restart cron 2>/dev/null || true
fi

if [ -d "$CONFIG_DIR" ]; then
	read -r -p "Remove configuration directory $CONFIG_DIR? [y/N] " answer
	case "$answer" in
		Y|y|Yes|yes)
			echo "Removing configuration."
			rm -rf "$CONFIG_DIR"
			;;
		*)
			echo "Keeping configuration in $CONFIG_DIR."
			;;
	esac
fi

systemctl daemon-reload
systemctl reset-failed displaycameras 2>/dev/null || true

echo "Uninstall complete."