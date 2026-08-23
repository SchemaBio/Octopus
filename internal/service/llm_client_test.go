package service

import (
	"context"
	"errors"
	"net"
	"net/http"
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

func TestLLMClientChatPropagatesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewLLMClient("http://llm.test/v1", "test-key", "test-model", AllowPrivateLLMEndpoints(true))
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})}

	_, err := client.Chat(ctx, []chatMessage{{Role: "user", Content: "test"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Chat error = %v, want context cancellation", err)
	}
}

func TestLLMClientChatStreamPropagatesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewLLMClient("http://llm.test/v1", "test-key", "test-model", AllowPrivateLLMEndpoints(true))
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})}

	err := client.ChatStream(ctx, []chatMessage{{Role: "user", Content: "test"}}, func(string) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ChatStream error = %v, want context cancellation", err)
	}
}
