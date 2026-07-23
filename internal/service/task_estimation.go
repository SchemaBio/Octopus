package service

import "math"

const (
	defaultTaskEstimatedMinutes = 60
	pairedFastqBaselineBytes    = int64(5 * 1024 * 1024 * 1024)
)

func estimateTaskMinutesFromBytes(totalBytes int64) int {
	if totalBytes <= 0 {
		return defaultTaskEstimatedMinutes
	}
	minutes := int(math.Ceil(float64(totalBytes) * defaultTaskEstimatedMinutes / float64(pairedFastqBaselineBytes)))
	if minutes < 1 {
		return 1
	}
	return minutes
}
