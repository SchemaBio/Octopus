package handler

import (
	"errors"
	"testing"
)

func TestSanitizeAuthError(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		expected string
	}{
		{
			name:     "nil error returns authentication failed",
			input:    nil,
			expected: "authentication failed",
		},
		{
			name:     "email already exists is preserved",
			input:    errors.New("email already exists"),
			expected: "email already exists",
		},
		{
			name:     "invalid email or password is preserved",
			input:    errors.New("invalid email or password"),
			expected: "invalid email or password",
		},
		{
			name:     "invalid credentials mapped to invalid email or password",
			input:    errors.New("invalid credentials"),
			expected: "invalid email or password",
		},
		{
			name:     "wrong password mapped to invalid email or password",
			input:    errors.New("wrong password"),
			expected: "invalid email or password",
		},
		{
			name:     "user not found mapped to invalid email or password",
			input:    errors.New("user not found"),
			expected: "invalid email or password",
		},
		{
			name:     "account is disabled mapped to invalid email or password",
			input:    errors.New("account is disabled"),
			expected: "invalid email or password",
		},
		{
			name:     "account is not active mapped to invalid email or password",
			input:    errors.New("account is not active"),
			expected: "invalid email or password",
		},
		{
			name:     "user account is disabled mapped to invalid email or password",
			input:    errors.New("user account is disabled"),
			expected: "invalid email or password",
		},
		{
			name:     "unknown database error mapped to generic authentication failed",
			input:    errors.New("pq: connection refused"),
			expected: "authentication failed",
		},
		{
			name:     "unknown internal error mapped to generic authentication failed",
			input:    errors.New("unexpected error in token generation"),
			expected: "authentication failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeAuthError(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeAuthError(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
