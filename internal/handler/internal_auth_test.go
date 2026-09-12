package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestValidMachineCallbackMAC(t *testing.T) {
	secret := "callback-secret"
	ts := "1700000000"
	body := []byte(`{"ok":true}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	now := time.Unix(1700000000, 0)

	if !validMachineCallbackMAC(secret, ts, sig, body, now) {
		t.Fatal("expected matching callback MAC to be accepted")
	}
	if validMachineCallbackMAC(secret, ts, sig, []byte(`{"ok":false}`), now) {
		t.Fatal("expected body mismatch to be rejected")
	}
	if validMachineCallbackMAC(secret, ts, sig, body, now.Add(10*time.Minute)) {
		t.Fatal("expected stale timestamp to be rejected")
	}
}
