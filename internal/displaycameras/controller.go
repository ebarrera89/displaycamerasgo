package displaycameras

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const defaultStateDir = "/run/displaycameras"

type ControllerOptions struct {
	Output   io.Writer
	StateDir string
}

type Controller struct {
	config   *Config
	output   io.Writer
	stateDir string
}

func NewController(config *Config, options ControllerOptions) *Controller {
	output := options.Output
	if output == nil {
		output = ioutil.Discard
	}
	stateDir := options.StateDir
	if stateDir == "" {
		stateDir = defaultStateDir
	}
	return &Controller{config: config, output: output, stateDir: stateDir}
}

func (controller *Controller) Run() error {
	if err := controller.Start(); err != nil {
		controller.Stop()
		return err
	}

	defer controller.Stop()
	repairTicker := time.NewTicker(time.Duration(controller.config.RepairIntervalSeconds) * time.Second)
	defer repairTicker.Stop()

	var rotateTicker *time.Ticker
	var rotateChannel <-chan time.Time
	if controller.config.Rotate {
		rotateTicker = time.NewTicker(time.Duration(controller.config.RotateDelaySeconds) * time.Second)
		rotateChannel = rotateTicker.C
		defer rotateTicker.Stop()
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	for {
		select {
		case <-repairTicker.C:
			if err := controller.Repair(false); err != nil {
				controller.log("repair failed: %v\n", err)
			}
		case <-rotateChannel:
			if err := controller.Rotate(controller.config.SequenceStep > 1); err != nil {
				controller.log("rotate failed: %v\n", err)
			}
		case signal := <-signals:
			controller.log("received %s, stopping\n", signal)
			return nil
		}
	}
}

func (controller *Controller) Start() error {
	if err := controller.ensureRuntime(); err != nil {
		return err
	}
	if controller.isActive() {
		return fmt.Errorf("displaycameras is already active")
	}

	layoutName, layout := controller.activeLayout()
	controller.log("Using layout %s\n", layoutName)

	if controller.config.Blank {
		controller.log("Blanking screen\n")
		if err := controller.startCommand("fbi", []string{"--noverbose", "-T", "2", controller.config.BlankImage}, nil); err != nil {
			return err
		}
	}

	sequence := controller.readSequence()
	startupFailure := false
	feedFailure := false

	for index, camera := range controller.config.Cameras {
		windowIndex := (index + sequence) % len(controller.config.Cameras)
		if err := controller.startCamera(camera, layout.Windows[windowIndex]); err != nil {
			controller.log("%s failed to start: %v\n", camera.Name, err)
			startupFailure = true
			continue
		}
		if !controller.waitForPlaying(camera) {
			startupFailure = true
		}
		if !startupFailure {
			time.Sleep(time.Duration(controller.config.FeedSleepSeconds) * time.Second)
		}
		if !controller.waitForPlayback(camera) {
			feedFailure = true
		}
		if controller.cameraHealthy(camera) {
			controller.log("%s started\n", camera.Name)
		} else {
			controller.log("%s failed playback\n", camera.Name)
		}
	}

	if err := controller.writeActive(); err != nil {
		return err
	}

	if startupFailure || feedFailure {
		controller.log("Running one repair pass for failed feeds\n")
		return controller.Repair(true)
	}

	return nil
}

func (controller *Controller) Stop() error {
	os.Remove(controller.activeFile())
	os.Remove(controller.sequenceFile())
	os.Remove(controller.netStateFile())
	files, _ := filepath.Glob(filepath.Join(controller.stateDir, "position.*"))
	for _, file := range files {
		os.Remove(file)
	}

	for _, camera := range controller.config.Cameras {
		controller.quit(camera.DBusName)
	}

	time.Sleep(2 * time.Second)
	if controller.config.Blank {
		controller.runIgnoringError("pkill", "fbi")
	}
	controller.runIgnoringError("killall", "omxplayer.bin")
	controller.log("Camera display stopped\n")
	return nil
}

func (controller *Controller) Restart() error {
	if err := controller.Stop(); err != nil {
		return err
	}
	time.Sleep(time.Second)
	return controller.Start()
}

func (controller *Controller) Repair(startup bool) error {
	if !controller.isActive() {
		return nil
	}

	networkRestored, networkUp := controller.networkState()
	if !networkUp {
		controller.log("Network unreachable, skipping repair\n")
		return nil
	}
	if networkRestored {
		controller.log("Network restored after outage, performing full restart\n")
		return controller.Restart()
	}

	omxCount := controller.omxplayerCount()
	if omxCount > len(controller.config.Cameras) {
		controller.log("Too many omxplayer instances (%d), restarting\n", omxCount)
		return controller.Restart()
	}

	_, layout := controller.activeLayout()
	sequence := controller.readSequence()
	for index, camera := range controller.config.Cameras {
		playStatus, _ := controller.playStatus(camera.DBusName)
		position, _ := controller.position(camera.DBusName)
		frozen := false
		positionFile := controller.positionFile(camera.DBusName)

		if playStatus == "Playing" && position != "0s" {
			previous, err := ioutil.ReadFile(positionFile)
			if err == nil && strings.TrimSpace(string(previous)) == position {
				controller.log("Detected frozen stream for %s at %s\n", camera.Name, position)
				frozen = true
			}
			ioutil.WriteFile(positionFile, []byte(position), 0644)
		}

		if playStatus == "Playing" && position != "0s" && !frozen {
			continue
		}

		os.Remove(positionFile)
		controller.quit(camera.DBusName)
		windowIndex := (index + sequence) % len(controller.config.Cameras)
		if err := controller.startCamera(camera, layout.Windows[windowIndex]); err != nil {
			controller.log("%s failed to restart: %v\n", camera.Name, err)
			continue
		}
		controller.waitForPlaying(camera)
		controller.waitForPlayback(camera)
		if controller.cameraHealthy(camera) {
			controller.log("%s restarted\n", camera.Name)
		} else {
			controller.log("%s failed playback\n", camera.Name)
		}
	}

	return nil
}

func (controller *Controller) Status() error {
	for _, camera := range controller.config.Cameras {
		status, err := controller.status(camera.DBusName)
		if err != nil || !strings.HasPrefix(status, "Playing") {
			controller.log("%s is NOT playing\n", camera.Name)
			continue
		}
		controller.log("%s is %s\n", camera.Name, status)
	}
	return nil
}

func (controller *Controller) Positions() error {
	for _, camera := range controller.config.Cameras {
		position, err := controller.position(camera.DBusName)
		if err != nil {
			position = "unknown"
		}
		controller.log("%s %s\n", position, camera.Name)
	}
	return nil
}

func (controller *Controller) Rotate(reverse bool) error {
	if !controller.isActive() {
		return nil
	}

	_, layout := controller.activeLayout()
	sequence := controller.readSequence()
	sequence = nextSequence(sequence, controller.config.SequenceStep, len(controller.config.Cameras), reverse)

	for index, camera := range controller.config.Cameras {
		windowIndex := (index + sequence) % len(controller.config.Cameras)
		if err := controller.setVideoPosition(camera.DBusName, layout.Windows[windowIndex].Position); err != nil {
			return err
		}
	}
	return controller.writeSequence(sequence)
}

func nextSequence(current int, step int, count int, reverse bool) int {
	if count < 1 {
		return 0
	}
	if reverse {
		return (current + step) % count
	}
	next := (current - step) % count
	if next < 0 {
		next += count
	}
	return next
}

func (controller *Controller) activeLayout() (string, Layout) {
	if controller.config.DisplayDetect {
		mode, err := controller.detectDisplayMode()
		if err == nil {
			if layout, ok := controller.config.Layouts[mode]; ok {
				return mode, layout
			}
			controller.log("No layout found for detected resolution %s, using default\n", mode)
		} else {
			controller.log("Display detection failed: %v\n", err)
		}
	}
	return "default", controller.config.Layouts["default"]
}

func (controller *Controller) detectDisplayMode() (string, error) {
	data, err := ioutil.ReadFile("/boot/config.txt")
	if err == nil && !hasDisableOverscan(data) {
		return "", fmt.Errorf("disable_overscan=1 is not active in /boot/config.txt")
	}
	output, err := exec.Command("fbset", "-s").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "mode ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			return strings.Trim(fields[1], "\""), nil
		}
	}
	return "", fmt.Errorf("fbset output did not include a mode line")
}

func hasDisableOverscan(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if line == "disable_overscan=1" {
			return true
		}
	}
	return false
}

func (controller *Controller) ensureRuntime() error {
	if err := os.MkdirAll(controller.stateDir, 0755); err != nil {
		return err
	}
	for _, binary := range []string{"omxplayer", "dbus-send", "ping", "pgrep", "killall"} {
		if _, err := exec.LookPath(binary); err != nil {
			return fmt.Errorf("%s is required: %v", binary, err)
		}
	}
	if controller.config.Blank {
		if _, err := exec.LookPath("fbi"); err != nil {
			return fmt.Errorf("fbi is required when blank is enabled: %v", err)
		}
	}
	return nil
}

func (controller *Controller) startCamera(camera Camera, window Window) error {
	controller.log("Starting omxplayer for %s in %s\n", camera.Name, window.Name)
	args := []string{
		"--no-keys",
		"--no-osd",
		"--avdict", "rtsp_transport:tcp",
		"--win", window.Position,
		camera.URL,
		"--live",
		"-n", "-1",
		"--timeout", strconv.Itoa(controller.config.OMXTimeoutSeconds),
		"--dbus_name", "org.mpris.MediaPlayer2.omxplayer." + camera.DBusName,
	}
	if err := controller.startCommand("omxplayer", args, nil); err != nil {
		return err
	}
	time.Sleep(time.Duration(controller.config.StartSleepSeconds) * time.Second)
	return nil
}

func (controller *Controller) startCommand(name string, args []string, env []string) error {
	command := exec.Command(name, args...)
	if env != nil {
		command.Env = append(os.Environ(), env...)
	}
	command.Stdout = ioutil.Discard
	command.Stderr = ioutil.Discard
	if err := command.Start(); err != nil {
		return err
	}
	go command.Wait()
	return nil
}

func (controller *Controller) waitForPlaying(camera Camera) bool {
	for attempt := 0; attempt <= controller.config.RetrySeconds; attempt++ {
		status, _ := controller.playStatus(camera.DBusName)
		if status == "Playing" {
			return true
		}
		controller.log("Waiting for %s omxplayer startup %d\n", camera.Name, attempt)
		time.Sleep(time.Second)
	}
	return false
}

func (controller *Controller) waitForPlayback(camera Camera) bool {
	for attempt := 0; attempt <= controller.config.RetrySeconds; attempt++ {
		position, _ := controller.position(camera.DBusName)
		if position != "" && position != "0s" {
			return true
		}
		controller.log("Waiting for %s playback %d\n", camera.Name, attempt)
		time.Sleep(time.Second)
	}
	return false
}

func (controller *Controller) cameraHealthy(camera Camera) bool {
	status, _ := controller.playStatus(camera.DBusName)
	position, _ := controller.position(camera.DBusName)
	return status == "Playing" && position != "" && position != "0s"
}

func (controller *Controller) networkState() (bool, bool) {
	host := controller.config.NetcheckHost
	if host == "" && len(controller.config.Cameras) > 0 {
		host = streamHost(controller.config.Cameras[0].URL)
	}
	if host == "" {
		return false, true
	}

	if err := exec.Command("ping", "-c", "1", "-W", "2", host).Run(); err != nil {
		ioutil.WriteFile(controller.netStateFile(), []byte("down"), 0644)
		return false, false
	}

	previous, err := ioutil.ReadFile(controller.netStateFile())
	ioutil.WriteFile(controller.netStateFile(), []byte("up"), 0644)
	return err == nil && strings.TrimSpace(string(previous)) == "down", true
}

func streamHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	trimmed := strings.TrimPrefix(rawURL, "rtsp://")
	if at := strings.LastIndex(trimmed, "@"); at >= 0 {
		trimmed = trimmed[at+1:]
	}
	for _, separator := range []string{":", "/"} {
		if index := strings.Index(trimmed, separator); index >= 0 {
			trimmed = trimmed[:index]
		}
	}
	return trimmed
}

func (controller *Controller) omxplayerCount() int {
	output, err := exec.Command("pgrep", "-c", "omxplayer.bin").Output()
	if err != nil {
		return 0
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return 0
	}
	return count
}

func (controller *Controller) runIgnoringError(name string, args ...string) {
	exec.Command(name, args...).Run()
}

func (controller *Controller) writeActive() error {
	if err := os.MkdirAll(controller.stateDir, 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(controller.activeFile(), []byte(strconv.Itoa(os.Getpid())), 0644)
}

func (controller *Controller) isActive() bool {
	_, err := os.Stat(controller.activeFile())
	return err == nil
}

func (controller *Controller) readSequence() int {
	data, err := ioutil.ReadFile(controller.sequenceFile())
	if err != nil {
		return 0
	}
	sequence, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || sequence < 0 || sequence >= len(controller.config.Cameras) {
		return 0
	}
	return sequence
}

func (controller *Controller) writeSequence(sequence int) error {
	if err := os.MkdirAll(controller.stateDir, 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(controller.sequenceFile(), []byte(strconv.Itoa(sequence)), 0644)
}

func (controller *Controller) activeFile() string {
	return filepath.Join(controller.stateDir, "active")
}

func (controller *Controller) sequenceFile() string {
	return filepath.Join(controller.stateDir, "sequence")
}

func (controller *Controller) netStateFile() string {
	return filepath.Join(controller.stateDir, "network")
}

func (controller *Controller) positionFile(cameraName string) string {
	return filepath.Join(controller.stateDir, "position."+cameraName)
}

func (controller *Controller) log(format string, args ...interface{}) {
	fmt.Fprintf(controller.output, format, args...)
}