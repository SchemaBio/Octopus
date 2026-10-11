package assistance

import (
	"strings"
	"testing"
)

func TestOptionalAuditReason(t *testing.T) {
	for _, reason := range []string{"", "  ", "调整", strings.Repeat("原", 500)} {
		if !ValidateReason(reason) {
			t.Fatal("optional reason was rejected")
		}
	}
	if ValidateReason(strings.Repeat("原", 501)) {
		t.Fatal("oversized reason was accepted")
	}
}
