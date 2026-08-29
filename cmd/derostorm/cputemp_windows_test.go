//go:build windows

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The hardware-monitor source is the only CPU temperature most Windows
// machines can offer, and it is the one this package can test end to end: the
// contract is an HTTP endpoint serving a JSON tree, so a test can serve one.
//
// What is being pinned is the awkward part -- picking the CPU sensor out of a
// tree that also contains GPU, drive and motherboard temperatures, and reading
// a value column that is formatted with the machine's locale.

const lhmSample = `{
  "Text": "Sensor",
  "Children": [{
    "Text": "MININGBOX",
    "Children": [
      {
        "Text": "NVIDIA GeForce RTX 5080",
        "Children": [{
          "Text": "Temperatures",
          "Children": [
            {"Text": "GPU Core", "Value": "66.0 °C"},
            {"Text": "GPU Hot Spot", "Value": "81.0 °C"}
          ]
        }]
      },
      {
        "Text": "AMD Ryzen 7 9800X3D",
        "Children": [{
          "Text": "Temperatures",
          "Children": [
            {"Text": "Core Max", "Value": "84.1 °C"},
            {"Text": "Core (Tctl/Tdie)", "Value": "71,5 °C"},
            {"Text": "Core Average", "Value": "68.9 °C"}
          ]
        }]
      },
      {
        "Text": "WDC WD_BLACK SN850X",
        "Children": [{
          "Text": "Temperatures",
          "Children": [{"Text": "Temperature", "Value": "44.0 °C"}]
        }]
      }
    ]
  }]
}`

func TestMonitorSourcePicksTheCPUPackage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(lhmSample))
	}))
	defer srv.Close()
	t.Setenv("DEROSTORM_CPU_TEMP_URL", srv.URL)

	c, src, ok := readCPUTempMonitor()
	if !ok {
		t.Fatal("could not read a temperature from a well-formed tree")
	}
	// Tctl/Tdie outranks Core Max and Core Average, and the comma in "71,5" is
	// a decimal point: the monitors format with the machine's locale, and no
	// temperature has thousands.
	if c < 71.4 || c > 71.6 {
		t.Errorf("got %.2f°C, want the Tctl/Tdie reading of 71.5", c)
	}
	if src == "" {
		t.Error("a working source must name itself")
	}
}

func TestMonitorSourceRejectsRubbish(t *testing.T) {
	for _, body := range []string{
		`not json at all`,
		`{"Text":"Sensor","Children":[]}`,               // nothing to find
		`{"Text":"Core (Tctl/Tdie)","Value":"1200 °C"}`, // not a temperature
		`{"Text":"Core (Tctl/Tdie)","Value":"71.5 W"}`,  // not even Celsius
		`{"Text":"GPU Core","Value":"66.0 °C"}`,         // a GPU, not the CPU
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(body))
		}))
		t.Setenv("DEROSTORM_CPU_TEMP_URL", srv.URL)
		if _, _, ok := readCPUTempMonitor(); ok {
			t.Errorf("accepted a reading from %q", body)
		}
		srv.Close()
	}
}

func TestMonitorSourceIsQuietWhenNothingIsListening(t *testing.T) {
	// A closed port must be a fast, silent miss: this runs on every machine
	// with no monitor installed, once a minute, forever.
	t.Setenv("DEROSTORM_CPU_TEMP_URL", "http://127.0.0.1:1/data.json")
	if _, _, ok := readCPUTempMonitor(); ok {
		t.Error("claimed a reading from a port with nothing behind it")
	}
}

func TestParseCelsius(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
		ok   bool
	}{
		{"55.6 °C", 55.6, true},
		{"71,5 °C", 71.5, true},
		{"  40 °C ", 40, true},
		{"-5.0 °C", -5, true},
		{"215.0 W", 0, false},
		{"", 0, false},
		{"°C", 0, false},
	} {
		got, ok := parseCelsius(c.in)
		if ok != c.ok {
			t.Errorf("parseCelsius(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && (got < c.want-0.01 || got > c.want+0.01) {
			t.Errorf("parseCelsius(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// The ACPI zone is the zero-setup source, and the trap it exists to avoid is a
// board that publishes a fixed placeholder. It may legitimately find nothing;
// what it must never do is return one of those placeholders.
func TestThermalZoneIsPlausibleOrAbsent(t *testing.T) {
	c, src, ok := readCPUTempThermalZone()
	if !ok {
		t.Skip("no usable ACPI thermal zone on this machine")
	}
	if !plausibleCPUTemp(c) {
		t.Errorf("returned %.1f°C from %q, which the filter should have rejected", c, src)
	}
}
