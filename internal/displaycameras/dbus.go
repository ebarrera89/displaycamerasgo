package displaycameras

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func (controller *Controller) playStatus(cameraName string) (string, error) {
	return controller.dbusValue(cameraName, "org.freedesktop.DBus.Properties.PlaybackStatus")
}

func (controller *Controller) status(cameraName string) (string, error) {
	playStatus, err := controller.playStatus(cameraName)
	if err != nil {
		return "", err
	}
	source, err := controller.dbusValue(cameraName, "org.mpris.MediaPlayer2.Player.GetSource")
	if err != nil {
		return "", err
	}
	position, err := controller.position(cameraName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s\nat %s", playStatus, source, strings.TrimSuffix(position, "s")+"sec"), nil
}

func (controller *Controller) position(cameraName string) (string, error) {
	output, err := controller.dbusValue(cameraName, "org.freedesktop.DBus.Properties.Position")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return "", fmt.Errorf("empty position response")
	}
	raw := fields[len(fields)-1]
	microseconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%ds", microseconds/1000000), nil
}

func (controller *Controller) setVideoPosition(cameraName string, position string) error {
	_, err := controller.dbus(cameraName, "org.mpris.MediaPlayer2.Player.VideoPos", "objpath:/not/used", "string:"+position)
	return err
}

func (controller *Controller) quit(cameraName string) {
	controller.dbus(cameraName, "org.mpris.MediaPlayer2.Quit")
}

func (controller *Controller) dbusValue(cameraName string, method string) (string, error) {
	output, err := controller.dbus(cameraName, method)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (controller *Controller) dbus(cameraName string, method string, args ...string) (string, error) {
	env, err := omxDBusEnv()
	if err != nil {
		return "", err
	}
	destination := "org.mpris.MediaPlayer2.omxplayer." + cameraName
	commandArgs := []string{
		"--print-reply=literal",
		"--session",
		"--reply-timeout=500",
		"--dest=" + destination,
		"/org/mpris/MediaPlayer2",
		method,
	}
	commandArgs = append(commandArgs, args...)
	command := exec.Command("dbus-send", commandArgs...)
	command.Env = append(os.Environ(), env...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func omxDBusEnv() ([]string, error) {
	user := os.Getenv("USER")
	if user == "" {
		user = "root"
	}
	addressPath := "/tmp/omxplayerdbus." + user
	pidPath := "/tmp/omxplayerdbus." + user + ".pid"
	address, err := ioutil.ReadFile(addressPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %v", addressPath, err)
	}
	pid, err := ioutil.ReadFile(pidPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %v", pidPath, err)
	}
	return []string{
		"DBUS_SESSION_BUS_ADDRESS=" + strings.TrimSpace(string(address)),
		"DBUS_SESSION_BUS_PID=" + strings.TrimSpace(string(pid)),
	}, nil
}