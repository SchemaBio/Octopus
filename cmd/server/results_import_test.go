package main

import "testing"

func TestResultsImportCommandRequiresTaskAndAttemptBeforeDatabaseAccess(t *testing.T) {
	if err := runResultsImportCommand(nil); err == nil {
		t.Fatal("result import command accepted missing task and attempt")
	}
	if err := runResultsImportCommand([]string{"--task", "task-id"}); err == nil {
		t.Fatal("result import command accepted a missing attempt")
	}
}
