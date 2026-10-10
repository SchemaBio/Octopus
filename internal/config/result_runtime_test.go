package config

import (
	"strings"
	"testing"
)

func TestRejectsRemovedQueryRuntimeEnvironment(t *testing.T) {
	names := []string{"PARQUET_QUERY_URL", "RESULT_ENGINE_BACKEND", "SVC_ENGINE_BACKEND"}
	for _, name := range names {
		t.Setenv(name, "")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "obsolete")
			err := ValidateStartup(&Config{})
			if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "removed") {
				t.Fatalf("missing migration diagnosis: %v", err)
			}
		})
	}
}
