package main

import "testing"

func TestExampleTemplateCommandRejectsSelfHostedBeforeDatabase(t *testing.T) {
	t.Setenv("DEPLOYMENT_EDITION", "opensource")
	if err := runExampleTemplateCommand([]string{"-disable"}); err == nil {
		t.Fatal("self-hosted template command enabled")
	}
}
