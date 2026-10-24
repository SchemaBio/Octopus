package service

import "errors"

// Retained for compatibility with callers matching the historical error.
var ErrFamilyAnalysisUnavailable = errors.New("family analysis is not available yet")

func requireAnalysisAvailable(template string) error { return nil }
