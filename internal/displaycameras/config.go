package displaycameras

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	Path                  string            `json:"-"`
	Blank                 bool              `json:"blank"`
	BlankImage            string            `json:"blank_image"`
	OMXTimeoutSeconds     int               `json:"omx_timeout_seconds"`
	StartSleepSeconds     int               `json:"start_sleep_seconds"`
	FeedSleepSeconds      int               `json:"feed_sleep_seconds"`
	RetrySeconds          int               `json:"retry_seconds"`
	DisplayDetect         bool              `json:"display_detect"`
	Rotate                bool              `json:"rotate"`
	RotateDelaySeconds    int               `json:"rotate_delay_seconds"`
	SequenceStep          int               `json:"sequence_step"`
	RepairIntervalSeconds int               `json:"repair_interval_seconds"`
	NetcheckHost          string            `json:"netcheck_host"`
	Layouts               map[string]Layout `json:"layouts"`
	Cameras               []Camera          `json:"cameras"`
}

type Layout struct {
	Windows []Window `json:"windows"`
}

type Window struct {
	Name     string `json:"name"`
	Position string `json:"position"`
}

type Camera struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	DBusName string `json:"dbus_name,omitempty"`
}

var dbusNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func LoadConfig(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %v", path, err)
	}

	config := &Config{Path: path}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(config); err != nil {
		return nil, fmt.Errorf("parse config %s: %v", path, err)
	}

	config.applyDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}

func (config *Config) applyDefaults() {
	if config.BlankImage == "" {
		config.BlankImage = "/usr/local/share/displaycameras/black.png"
	}
	if config.OMXTimeoutSeconds == 0 {
		config.OMXTimeoutSeconds = 30
	}
	if config.StartSleepSeconds == 0 {
		config.StartSleepSeconds = 3
	}
	if config.FeedSleepSeconds == 0 {
		config.FeedSleepSeconds = 1
	}
	if config.RetrySeconds == 0 {
		config.RetrySeconds = 5
	}
	if config.RotateDelaySeconds == 0 {
		config.RotateDelaySeconds = 5
	}
	if config.SequenceStep == 0 {
		config.SequenceStep = 1
	}
	if config.RepairIntervalSeconds == 0 {
		config.RepairIntervalSeconds = 60
	}
}

func (config *Config) Validate() error {
	if len(config.Cameras) == 0 {
		return fmt.Errorf("config must define at least one camera")
	}
	if len(config.Layouts) == 0 {
		return fmt.Errorf("config must define at least one layout")
	}
	if _, ok := config.Layouts["default"]; !ok {
		return fmt.Errorf("config must define layouts.default")
	}
	if config.SequenceStep < 1 || config.SequenceStep > len(config.Cameras) {
		return fmt.Errorf("sequence_step must be between 1 and the number of cameras")
	}
	if config.RepairIntervalSeconds < 1 {
		return fmt.Errorf("repair_interval_seconds must be greater than zero")
	}

	cameraNames := make(map[string]bool)
	dbusNames := make(map[string]bool)
	for index, camera := range config.Cameras {
		camera.Name = strings.TrimSpace(camera.Name)
		if camera.Name == "" {
			return fmt.Errorf("cameras[%d].name is required", index)
		}
		if cameraNames[camera.Name] {
			return fmt.Errorf("duplicate camera name %q", camera.Name)
		}
		cameraNames[camera.Name] = true
		if camera.URL == "" {
			return fmt.Errorf("camera %q must define a url", camera.Name)
		}

		camera.DBusName = strings.TrimSpace(camera.DBusName)
		if camera.DBusName != "" {
			if !dbusNamePattern.MatchString(camera.DBusName) {
				return fmt.Errorf("camera %q dbus_name must match %s", camera.Name, dbusNamePattern.String())
			}
			if dbusNames[camera.DBusName] {
				return fmt.Errorf("duplicate dbus_name %q", camera.DBusName)
			}
		} else {
			camera.DBusName = safeDBusName(camera.Name, index, dbusNames)
		}
		dbusNames[camera.DBusName] = true
		config.Cameras[index] = camera
	}

	for name, layout := range config.Layouts {
		if len(layout.Windows) < len(config.Cameras) {
			return fmt.Errorf("layout %q defines %d windows for %d cameras", name, len(layout.Windows), len(config.Cameras))
		}
		windowNames := make(map[string]bool)
		for index, window := range layout.Windows {
			if window.Name == "" {
				return fmt.Errorf("layout %q windows[%d].name is required", name, index)
			}
			if windowNames[window.Name] {
				return fmt.Errorf("layout %q has duplicate window name %q", name, window.Name)
			}
			windowNames[window.Name] = true
			if !validPosition(window.Position) {
				return fmt.Errorf("layout %q window %q position must be four integers", name, window.Name)
			}
		}
	}

	return nil
}

func safeDBusName(name string, index int, used map[string]bool) string {
	base := dbusSafeBase(name)
	if base == "" || base == "_" {
		base = fmt.Sprintf("Camera%d", index+1)
	}
	if !used[base] {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s_%d", base, suffix)
		if !used[candidate] {
			return candidate
		}
	}
}

func dbusSafeBase(name string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
			builder.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			if builder.Len() == 0 {
				builder.WriteByte('_')
			}
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if builder.Len() > 0 && !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.TrimRight(builder.String(), "_")
}

func validPosition(position string) bool {
	fields := strings.Fields(position)
	if len(fields) != 4 {
		return false
	}
	for _, field := range fields {
		if _, err := strconv.Atoi(field); err != nil {
			return false
		}
	}
	return true
}