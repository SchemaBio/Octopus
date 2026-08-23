package handler

import (
	"strings"
	"testing"
)

func TestWriteSSEEventPrefixesEveryDataLine(t *testing.T) {
	var output strings.Builder
	payload := "first line\nevent: forged\ndata: forged\n"
	if err := writeSSEEvent(&output, "message", payload); err != nil {
		t.Fatalf("writeSSEEvent: %v", err)
	}

	want := "event: message\n" +
		"data: first line\n" +
		"data: event: forged\n" +
		"data: data: forged\n" +
		"data: \n\n"
	if output.String() != want {
		t.Fatalf("SSE output = %q, want %q", output.String(), want)
	}
	if strings.Contains(output.String(), "\nevent: forged\n") {
		t.Fatalf("payload injected an SSE event: %q", output.String())
	}
}
