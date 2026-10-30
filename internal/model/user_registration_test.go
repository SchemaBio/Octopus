package model

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
)

func TestRegisterRequestOptionalName(t *testing.T) {
	req := RegisterRequest{Email: "researcher@example.org", Password: "Test-Password-42"}
	if err := binding.Validator.ValidateStruct(req); err != nil {
		t.Fatalf("name must be optional: %v", err)
	}
	req.Email = ""
	if err := binding.Validator.ValidateStruct(req); err == nil {
		t.Fatal("email must remain required")
	}
}
