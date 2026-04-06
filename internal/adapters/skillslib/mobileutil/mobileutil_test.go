package mobileutil

import "testing"

func TestParseSimctlDevices(t *testing.T) {
	input := []byte(`{
  "devices": {
    "com.apple.CoreSimulator.SimRuntime.iOS-18-0": [
      {
        "udid": "AAA-BBB",
        "name": "iPhone 16",
        "state": "Booted",
        "isAvailable": true
      }
    ],
    "com.apple.CoreSimulator.SimRuntime.iOS-17-5": [
      {
        "udid": "CCC-DDD",
        "name": "iPhone 15",
        "state": "Shutdown",
        "isAvailable": true
      }
    ]
  }
}`)

	devices := ParseSimctlDevices(input)
	if len(devices) != 2 {
		t.Fatalf("len(devices)=%d want 2", len(devices))
	}
	if devices[0].UDID != "AAA-BBB" {
		t.Fatalf("first udid=%q want AAA-BBB", devices[0].UDID)
	}
	if devices[0].State != "booted" {
		t.Fatalf("first state=%q want booted", devices[0].State)
	}
	if devices[0].OSVersion != "iOS 18.0" {
		t.Fatalf("first os=%q want iOS 18.0", devices[0].OSVersion)
	}
	if devices[1].OSVersion != "iOS 17.5" {
		t.Fatalf("second os=%q want iOS 17.5", devices[1].OSVersion)
	}
}

func TestFormatSimctlRuntime(t *testing.T) {
	if got := formatSimctlRuntime("com.apple.CoreSimulator.SimRuntime.iOS-18-2"); got != "iOS 18.2" {
		t.Fatalf("got %q want iOS 18.2", got)
	}
	if got := formatSimctlRuntime("custom-runtime"); got != "custom-runtime" {
		t.Fatalf("got %q want custom-runtime", got)
	}
}
