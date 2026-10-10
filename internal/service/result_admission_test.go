package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestResultAdmissionNestedAndCancelled(t *testing.T) {
	ctx, release, err := admitResultOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, nestedRelease, err := admitResultOperation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nestedRelease()
	_, release2, err := admitResultOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = admitResultOperation(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	calls := 0
	b := &leasedExportBody{ReadCloser: io.NopCloser(strings.NewReader("csv")), release: func() { calls++ }}
	b.Close()
	b.Close()
	if calls != 1 {
		t.Fatal("export released its slot more than once")
	}
}
