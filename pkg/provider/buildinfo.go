package provider

import "time"

// BuildTime is injected at build time via:
// -ldflags "-X zerotwo/pkg/provider.BuildTime=YYYY-MM-DDTHH:MM:SSZ"
var BuildTime = "2026-01-01T00:00:00Z"

func BuildTimeFloor() time.Time {
	t, err := time.Parse(time.RFC3339, BuildTime)
	if err != nil {
		// Fallback to safe floor
		return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return t
}
