package displaycameras

import "testing"

func TestValidateAppliesSimpleConfig(t *testing.T) {
	config := &Config{
		Layouts: map[string]Layout{
			"default": {
				Windows: []Window{
					{Name: "one", Position: "0 0 99 99"},
				},
			},
		},
		Cameras: []Camera{{Name: "Frente Izquierda", URL: "rtsp://192.168.1.10/live"}},
	}
	config.applyDefaults()
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if config.Cameras[0].DBusName != "Frente_Izquierda" {
		t.Fatalf("expected generated dbus name, got %q", config.Cameras[0].DBusName)
	}
	if config.OMXTimeoutSeconds != 30 {
		t.Fatalf("expected omx timeout default, got %d", config.OMXTimeoutSeconds)
	}
}

func TestValidateRejectsInvalidDBusName(t *testing.T) {
	config := &Config{
		Layouts: map[string]Layout{
			"default": {
				Windows: []Window{{Name: "one", Position: "0 0 99 99"}},
			},
		},
		Cameras: []Camera{{Name: "Front", DBusName: "1-front", URL: "rtsp://192.168.1.10/live"}},
	}
	config.applyDefaults()
	if err := config.Validate(); err == nil {
		t.Fatal("expected invalid dbus_name to fail validation")
	}
}

func TestValidateMakesGeneratedDBusNamesUnique(t *testing.T) {
	config := &Config{
		Layouts: map[string]Layout{
			"default": {
				Windows: []Window{
					{Name: "one", Position: "0 0 99 99"},
					{Name: "two", Position: "100 0 199 99"},
				},
			},
		},
		Cameras: []Camera{
			{Name: "Front Door", URL: "rtsp://192.168.1.10/live"},
			{Name: "Front-Door", URL: "rtsp://192.168.1.11/live"},
		},
	}
	config.applyDefaults()
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if config.Cameras[0].DBusName != "Front_Door" || config.Cameras[1].DBusName != "Front_Door_2" {
		t.Fatalf("unexpected dbus names: %#v", config.Cameras)
	}
}

func TestNextSequence(t *testing.T) {
	if got := nextSequence(0, 1, 4, true); got != 1 {
		t.Fatalf("reverse rotation: got %d", got)
	}
	if got := nextSequence(0, 1, 4, false); got != 3 {
		t.Fatalf("forward rotation: got %d", got)
	}
	if got := nextSequence(3, 2, 4, true); got != 1 {
		t.Fatalf("stepped reverse rotation: got %d", got)
	}
}