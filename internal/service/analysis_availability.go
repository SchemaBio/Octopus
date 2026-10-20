package service

import (
	"errors"
	"strings"
)

// Keep family workflows and historical executions intact until public release.
var ErrFamilyAnalysisUnavailable = errors.New("family analysis is not available yet")

func requireAnalysisAvailable(template string) error {
	switch strings.ToLower(strings.TrimSpace(template)) {
	case "trio", "germline_trio":
		return ErrFamilyAnalysisUnavailable
	}
	return nil
}
