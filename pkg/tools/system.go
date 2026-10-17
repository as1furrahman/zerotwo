package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type SystemStatusTool struct {
	maxOutput int
}

func NewSystemStatusTool(maxOutput int) *SystemStatusTool {
	return &SystemStatusTool{maxOutput: maxOutput}
}

func (t *SystemStatusTool) Name() string { return "get_system_status" }
func (t *SystemStatusTool) Description() string {
	return "Read generic Linux system telemetry: memory usage (/proc/meminfo), CPU load (/proc/loadavg), thermals, battery, and disk storage."
}
func (t *SystemStatusTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}
func (t *SystemStatusTool) Run(_ string) (string, error) {
	var sb strings.Builder

	// 1. CPU Load
	if loadData, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(loadData))
		if len(parts) >= 3 {
			sb.WriteString(fmt.Sprintf("• CPU Load: 1m=%s, 5m=%s, 15m=%s\n", parts[0], parts[1], parts[2]))
		}
	}

	// 2. Memory
	if memData, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(memData), "\n")
		var total, avail, free string
		for _, line := range lines {
			if strings.HasPrefix(line, "MemTotal:") {
				total = strings.TrimSpace(strings.TrimPrefix(line, "MemTotal:"))
			} else if strings.HasPrefix(line, "MemAvailable:") {
				avail = strings.TrimSpace(strings.TrimPrefix(line, "MemAvailable:"))
			} else if strings.HasPrefix(line, "MemFree:") && avail == "" {
				free = strings.TrimSpace(strings.TrimPrefix(line, "MemFree:"))
			}
		}
		if avail == "" {
			avail = free
		}
		sb.WriteString(fmt.Sprintf("• Memory: Total=%s, Available=%s\n", total, avail))
	}

	// 3. Thermals
	thermalPaths, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	for i, tp := range thermalPaths {
		if data, err := os.ReadFile(tp); err == nil { // #nosec G304 -- sysfs telemetry read
			val, _ := strconv.Atoi(strings.TrimSpace(string(data)))
			if val > 1000 {
				sb.WriteString(fmt.Sprintf("• Thermal Zone %d: %.1f°C\n", i, float64(val)/1000.0))
			} else if val > 0 {
				sb.WriteString(fmt.Sprintf("• Thermal Zone %d: %d°C\n", i, val))
			}
		}
	}

	// 4. Power / Battery
	supplyPaths, _ := filepath.Glob("/sys/class/power_supply/*")
	for _, sp := range supplyPaths {
		capPath := filepath.Join(sp, "capacity")
		statPath := filepath.Join(sp, "status")
		base := filepath.Base(sp)

		var capStr, statStr string
		if cData, err := os.ReadFile(capPath); err == nil { // #nosec G304 -- sysfs telemetry read
			capStr = strings.TrimSpace(string(cData)) + "%"
		}
		if sData, err := os.ReadFile(statPath); err == nil { // #nosec G304 -- sysfs telemetry read
			statStr = strings.TrimSpace(string(sData))
		}

		if capStr != "" || statStr != "" {
			sb.WriteString(fmt.Sprintf("• Power (%s): %s %s\n", base, capStr, statStr))
		}
	}

	// 5. Disk storage (root /)
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil && stat.Bsize > 0 {
		bsize := uint64(stat.Bsize) // #nosec G115 -- safe positive block size
		totalBytes := stat.Blocks * bsize
		freeBytes := stat.Bavail * bsize
		sb.WriteString(fmt.Sprintf("• Root Storage (/): Total=%.1f GB, Free=%.1f GB\n",
			float64(totalBytes)/(1024*1024*1024), float64(freeBytes)/(1024*1024*1024)))
	}

	res := strings.TrimSpace(sb.String())
	if res == "" {
		res = "No standard Linux /proc or /sys telemetry found."
	}
	return TruncateOutput(res, t.maxOutput), nil
}
