package service

import (
	"net"
	"testing"
)

func TestValidatePublicHTTPSLLMEndpointRejectsPrivateIP(t *testing.T) {
	err := validatePublicHTTPSLLMEndpointWithResolver("https://llm.example.com/v1/chat/completions", func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	})
	if err == nil {
		t.Fatal("expected private/loopback LLM endpoint to be rejected")
	}
}

func TestValidatePublicHTTPSLLMEndpointRejectsHTTP(t *testing.T) {
	err := validatePublicHTTPSLLMEndpointWithResolver("http://llm.example.com/v1/chat/completions", func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	if err == nil {
		t.Fatal("expected non-HTTPS LLM endpoint to be rejected")
	}
}

func TestValidateLLMEndpointAllowsPrivateWhenExplicitlyEnabled(t *testing.T) {
	if err := validateLLMEndpoint("http://127.0.0.1:11434/v1/chat/completions", true); err != nil {
		t.Fatalf("expected debug/private endpoint to be allowed when explicitly enabled: %v", err)
	}
}
