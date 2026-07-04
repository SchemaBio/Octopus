package service

import (
	"fmt"
	"strings"
	"unicode"
)

var commonWeakPasswords = map[string]bool{
	"admin123":    true,
	"password":    true,
	"password123": true,
	"123456":      true,
	"12345678":    true,
	"qwerty123":   true,
	"changeme":    true,
}

// ValidatePasswordStrength enforces a small server-side baseline for local
// accounts. Frontend validation is only a convenience and must not be trusted.
func ValidatePasswordStrength(password string) error {
	if len([]rune(password)) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if commonWeakPasswords[strings.ToLower(strings.TrimSpace(password))] {
		return fmt.Errorf("password is too common")
	}
	return nil
}

// ValidateStrongAdminPassword is used for release-mode bootstrap credentials.
func ValidateStrongAdminPassword(password string) error {
	if err := ValidatePasswordStrength(password); err != nil {
		return err
	}
	if len([]rune(password)) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}
	classes := 0
	var hasLower, hasUpper, hasDigit, hasSymbol bool
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSymbol = true
		}
	}
	for _, ok := range []bool{hasLower, hasUpper, hasDigit, hasSymbol} {
		if ok {
			classes++
		}
	}
	if classes < 3 {
		return fmt.Errorf("password must include at least 3 of: lowercase, uppercase, digit, symbol")
	}
	return nil
}
