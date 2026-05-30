#!/bin/bash
set -e

# Run as root on Raspberry Pi OS Buster to install displaycameras.

if [[ $EUID -ne 0 ]]; then
	echo "This installer must be run as root."
	exit 1
fi

DIR=$(dirname "$(readlink -f "$0")")
CONFIG_DIR=/etc/displaycameras
CONFIG_FILE=$CONFIG_DIR/config.json
SERVICE_FILE=/etc/systemd/system/displaycameras.service
SHARE_DIR=/usr/local/share/displaycameras
GO_VERSION=${GO_VERSION:-1.22.12}
GO_ROOT=/usr/local/lib/displaycameras-go
GO_DOWNLOAD_BASE=https://go.dev/dl
BUILD_DIR=
APT_UPDATED=false

cleanup() {
	if [ -n "$BUILD_DIR" ] && [ -d "$BUILD_DIR" ]; then
		rm -rf "$BUILD_DIR"
	fi
}
trap cleanup EXIT

ensure_package() {
	local package="$1"
	if ! dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -q "install ok installed"; then
		update_package_lists
		echo "Installing $package"
		if ! apt-get install "$package" -y; then
			echo "Failed to install $package."
			echo "Check network access and the Buster entries in /etc/apt/sources.list and /etc/apt/sources.list.d/."
			exit 1
		fi
	fi
}

update_package_lists() {
	if [ "$APT_UPDATED" = "true" ]; then
		return
	fi
	echo "Updating apt package lists."
	if ! apt-get update; then
		echo "Failed to update apt package lists."
		echo "Check network access and the Buster entries in /etc/apt/sources.list and /etc/apt/sources.list.d/."
		exit 1
	fi
	APT_UPDATED=true
}

download_file() {
	local url="$1"
	local output="$2"
	if command -v wget >/dev/null 2>&1; then
		wget -O "$output" "$url"
		return
	fi
	if command -v curl >/dev/null 2>&1; then
		curl -L -o "$output" "$url"
		return
	fi
	echo "wget or curl is required to download Go from $GO_DOWNLOAD_BASE."
	exit 1
}

go_platform() {
	local arch
	arch=$(dpkg --print-architecture 2>/dev/null || uname -m)
	case "$arch" in
		armhf|armel|armv6l|armv7l)
			echo "linux-armv6l"
			;;
		arm64|aarch64)
			echo "linux-arm64"
			;;
		amd64|x86_64)
			echo "linux-amd64"
			;;
		*)
			echo "Unsupported architecture for official Go download: $arch" >&2
			exit 1
			;;
	esac
}

install_go() {
	if [ -x "$GO_ROOT/bin/go" ] && "$GO_ROOT/bin/go" version | grep -q "go$GO_VERSION "; then
		return
	fi

	local platform tarball url temp_dir
	platform=$(go_platform)
	tarball="go${GO_VERSION}.${platform}.tar.gz"
	url="$GO_DOWNLOAD_BASE/$tarball"
	temp_dir=$(mktemp -d)

	echo "Downloading Go $GO_VERSION from $url"
	download_file "$url" "$temp_dir/$tarball"

	echo "Installing Go $GO_VERSION to $GO_ROOT"
	rm -rf "$GO_ROOT"
	mkdir -p "$(dirname "$GO_ROOT")"
	tar -C "$temp_dir" -xzf "$temp_dir/$tarball"
	mv "$temp_dir/go" "$GO_ROOT"
	rm -rf "$temp_dir"
}

for package in omxplayer fbi dbus iputils-ping procps psmisc fbset; do
	ensure_package "$package"
done

install_go

echo "Building displaycameras."
BUILD_DIR=$(mktemp -d)
(cd "$DIR" && GO111MODULE=on "$GO_ROOT/bin/go" build -o "$BUILD_DIR/displaycameras" ./cmd/displaycameras)
install -o root -g root -m 0755 "$BUILD_DIR/displaycameras" /usr/bin/displaycameras

echo "Installing systemd service."
install -o root -g root -m 0644 "$DIR/displaycameras.service" "$SERVICE_FILE"

echo "Installing configuration."
install -d -o root -g root -m 0755 "$CONFIG_DIR"
if [ "$1" != "upgrade" ]; then
	if [ -e "$CONFIG_FILE" ]; then
		backup="$CONFIG_DIR/config.json.$(date +%Y%m%d%H%M%S).bak"
		cp -a "$CONFIG_FILE" "$backup"
		echo "Existing config backed up to $backup"
	fi
	install -o root -g root -m 0644 "$DIR/displaycameras.json" "$CONFIG_FILE"
elif [ ! -e "$CONFIG_FILE" ]; then
	install -o root -g root -m 0644 "$DIR/displaycameras.json" "$CONFIG_FILE"
fi

install -d -o root -g root -m 0755 "$SHARE_DIR"
if [ -r "$DIR/black.png" ]; then
	install -o root -g root -m 0644 "$DIR/black.png" "$SHARE_DIR/black.png"
else
	echo "black.png is missing; screen blanking will not work until blank_image points to a valid file."
fi

if [ -e /etc/cron.d/repaircameras ]; then
	echo "Removing legacy repair cron job."
	rm -f /etc/cron.d/repaircameras
	systemctl restart cron || true
fi

configure_boot() {
	if ! command -v raspi-config >/dev/null 2>&1; then
		return
	fi

	local sysmem gpumem physmem split current_overscan
	sysmem=$(free -m | awk '/^Mem:/ {print $2}')
	gpumem=$(raspi-config nonint get_config_var gpu_mem /boot/config.txt 2>/dev/null || echo 0)
	if [ -z "$gpumem" ]; then
		gpumem=0
	fi
	physmem=$((gpumem + sysmem))
	if [ "$physmem" -lt 500 ]; then
		split=96
	elif [ "$physmem" -lt 1000 ]; then
		split=192
	else
		split=256
	fi

	if [ "$gpumem" -lt "$split" ]; then
		echo "Setting gpu_mem allocation to ${split}MB"
		raspi-config nonint do_memory_split "$split"
	fi

	current_overscan=$(raspi-config nonint get_overscan 2>/dev/null || echo 1)
	if [ "$current_overscan" = "0" ]; then
		echo "Disabling display overscan compensation."
		raspi-config nonint do_overscan 1
	fi
}

if [ "$1" != "upgrade" ]; then
	configure_boot
fi

systemctl daemon-reload
systemctl enable displaycameras

echo "Installation successful."
echo "Edit $CONFIG_FILE, then start with: sudo systemctl start displaycameras"
