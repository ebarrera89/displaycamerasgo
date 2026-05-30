# displaycamerasgo

This is a Go rewrite of [displaycameras](https://github.com/Anonymousdog/displaycameras) that keeps the important runtime behavior: it still launches `omxplayer` for each RTSP feed and controls each instance through DBus. The main difference is configuration: one JSON file replaces the old split Bash configuration and layout files.

Use Raspberry Pi OS Buster. Bullseye and newer Raspberry Pi OS releases removed normal `omxplayer` support, so Buster is the practical target if `omxplayer` is mandatory.

## Why Go

Go is a good fit here because the Raspberry Pi can run one compiled binary built with only standard-library code. That keeps the runtime small and gives safer process/config handling than Bash.

The installer downloads the official Go ARM toolchain from `go.dev`, installs it privately under `/usr/local/lib/displaycameras-go`, and compiles `displaycameras` on the Raspberry Pi. After installation, systemd runs the compiled `/usr/bin/displaycameras` binary.

## Install Over SSH

From your development machine:

```sh
scp -r displaycamerasgo pi@raspberrypi.local:/tmp/displaycamerasgo
ssh pi@raspberrypi.local
```

On the Raspberry Pi:

```sh
cd /tmp/displaycamerasgo
sudo ./install.sh
sudo nano /etc/displaycameras/config.json
sudo /usr/bin/displaycameras --config /etc/displaycameras/config.json validate
sudo systemctl start displaycameras
```

For upgrades, keep your existing config:

```sh
cd /tmp/displaycamerasgo
sudo ./install.sh upgrade
sudo systemctl restart displaycameras
```

To uninstall:

```sh
cd /tmp/displaycamerasgo
sudo ./uninstall.sh
```

The uninstaller stops and disables the systemd service, removes the installed binary and service file, and asks before deleting `/etc/displaycameras`.

## Configuration

Edit `/etc/displaycameras/config.json`. The included `displaycameras.json` is a four-camera 1080p example.

Important fields:

- `cameras`: friendly camera names and RTSP URLs. Names can contain spaces, such as `Frente Izquierda`.
- `cameras[].dbus_name`: optional internal omxplayer/DBus name. Leave it unset unless you need a specific identifier; generated names are made safe automatically.
- `layouts.default.windows`: the `omxplayer --win` rectangles used for each feed.
- `display_detect`: when true, `fbset -s` is used and a matching layout key such as `1920x1080` can override `default`.
- `rotate`, `rotate_delay_seconds`, `sequence_step`: rotate feeds through configured windows.
- `repair_interval_seconds`: built-in watchdog interval. No cron job is needed.
- `netcheck_host`: optional host to ping before repair; if empty, the first RTSP host is used.

Useful commands:

```sh
sudo systemctl status displaycameras
sudo /usr/bin/displaycameras status
sudo /usr/bin/displaycameras positions
sudo /usr/bin/displaycameras repair
sudo systemctl stop displaycameras
```
