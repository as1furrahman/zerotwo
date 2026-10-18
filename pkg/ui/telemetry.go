package ui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Telemetry holds live system metrics for cyberdeck status displays.
type Telemetry struct {
	BatteryPct int
	TempC      int
	RAMFreeMB  int
	LoadAvg    float64
}

// GetTelemetry fetches live system metrics from /proc and /sys, falling back to defaults if unavailable.
func GetTelemetry() Telemetry {
	t := Telemetry{
		BatteryPct: 80,
		TempC:      45,
		RAMFreeMB:  204,
		LoadAvg:    0.31,
	}

	// 1. Load average from /proc/loadavg
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if l, err := strconv.ParseFloat(fields[0], 64); err == nil {
				t.LoadAvg = l
			}
		}
	}

	// 2. Battery percentage from /sys/class/power_supply/battery/capacity
	batPaths := []string{
		"/sys/class/power_supply/battery/capacity",
		"/sys/devices/system/yoshi_battery/batt_capacity",
	}
	for _, p := range batPaths {
		if data, err := os.ReadFile(p); err == nil {
			str := strings.TrimSpace(string(data))
			if b, err := strconv.Atoi(str); err == nil && b > 0 && b <= 100 {
				t.BatteryPct = b
				break
			}
		}
	}

	// 3. Thermal sensor from /sys/class/thermal/thermal_zone0/temp (Kindle or generic Linux)
	thermPaths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/devices/virtual/thermal/thermal_zone0/temp",
	}
	for _, p := range thermPaths {
		if data, err := os.ReadFile(p); err == nil {
			str := strings.TrimSpace(string(data))
			if tempVal, err := strconv.Atoi(str); err == nil && tempVal > 0 {
				if tempVal > 1000 {
					t.TempC = tempVal / 1000
				} else {
					t.TempC = tempVal
				}
				break
			}
		}
	}

	// 4. Memory available from /proc/meminfo
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemAvailable:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					if kb, err := strconv.Atoi(parts[1]); err == nil && kb > 0 {
						t.RAMFreeMB = kb / 1024
					}
				}
				break
			}
		}
	}

	return t
}

// BatteryGauge returns a 5-block gauge string like [■■■■□].
func (t Telemetry) BatteryGauge() string {
	pct := t.BatteryPct
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := (pct + 10) / 20
	if filled > 5 {
		filled = 5
	}
	return fmt.Sprintf("[%s%s]", strings.Repeat("■", filled), strings.Repeat("□", 5-filled))
}
