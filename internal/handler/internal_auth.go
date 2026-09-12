package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/gin-gonic/gin"
)

const (
	octopusCallbackTimestampHeader = "X-Octopus-Callback-Timestamp"
	octopusCallbackSignatureHeader = "X-Octopus-Callback-Signature"
	octopusCallbackMaxSkew         = 5 * time.Minute
)

func machineCallbackBearer(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return header
}

func machineCallbackSecret(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if secret := strings.TrimSpace(cfg.ExternalAuth.CallbackSecret); secret != "" {
		return secret
	}
	if cfg.Server.Mode != "release" {
		return strings.TrimSpace(cfg.ExternalAuth.SharedSecret)
	}
	return ""
}

func authenticateMachineCallback(c *gin.Context, cfg *config.Config, maxBody int64) ([]byte, bool) {
	secret := machineCallbackSecret(cfg)
	if cfg == nil || !cfg.ExternalAuth.Enabled || secret == "" {
		ErrorUnauthorized(c, "CVM callback authentication is disabled")
		return nil, false
	}
	token := machineCallbackBearer(c.GetHeader("Authorization"))
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		ErrorUnauthorized(c, "Invalid CVM callback credentials")
		return nil, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBody)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		ErrorBadRequest(c, "request body too large")
		return nil, false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	ts := strings.TrimSpace(c.GetHeader(octopusCallbackTimestampHeader))
	sig := strings.TrimSpace(c.GetHeader(octopusCallbackSignatureHeader))
	if !validMachineCallbackMAC(secret, ts, sig, body, time.Now()) {
		ErrorUnauthorized(c, "Invalid CVM callback signature")
		return nil, false
	}
	return body, true
}

func validMachineCallbackMAC(secret, timestamp, signature string, body []byte, now time.Time) bool {
	unix, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil || unix <= 0 {
		return false
	}
	issued := time.Unix(unix, 0)
	skew := now.Sub(issued)
	if skew < 0 {
		skew = -skew
	}
	if skew > octopusCallbackMaxSkew {
		return false
	}
	expected, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(expected) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hmac.Equal(expected, mac.Sum(nil))
}
