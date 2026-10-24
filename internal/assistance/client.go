package assistance

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type Client struct {
	BaseURL, Token string
	HTTP           *http.Client
}
type UpstreamError struct {
	Status int
	Code   string
}

func (e *UpstreamError) Error() string { return "SepiLens: " + e.Code }
func (c *Client) Call(ctx context.Context, method, path, key string, body any, out any) (int, error) {
	timeout := 15 * time.Second
	if path == "/v2/hpo/extract" {
		timeout = 330 * time.Second
	}
	if path == "/v2/providers/test" {
		timeout = 40 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, errors.New("SepiLens unavailable")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return resp.StatusCode, errors.New("invalid SepiLens response")
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error struct {
				Code string `json:"code"`
			}
		}
		_ = json.Unmarshal(raw, &e)
		return resp.StatusCode, &UpstreamError{resp.StatusCode, e.Error.Code}
	}
	if out != nil {
		if err = json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, errors.New("invalid SepiLens response")
		}
	}
	return resp.StatusCode, nil
}
func newClient() *Client {
	base := os.Getenv("AI_ASSISTANCE_BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:18081"
	}
	return &Client{base, readSecret("AI_ASSISTANCE_TOKEN"), &http.Client{Timeout: 330 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func readSecret(name string) string {
	if path := os.Getenv(name + "_FILE"); path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	return os.Getenv(name)
}
func newCipher() (cipher.AEAD, error) {
	key, e := base64.StdEncoding.DecodeString(readSecret("AI_ASSISTANCE_MASTER_KEY"))
	if e != nil || len(key) != 32 {
		return nil, errors.New("AI encryption key unavailable")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func seal(c cipher.AEAD, scope string, value any) (string, error) {
	b, e := json.Marshal(value)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, c.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(c.Seal(nonce, nonce, b, []byte(scope))), nil
}
func open(c cipher.AEAD, scope, value string, out any) error {
	b, e := base64.StdEncoding.DecodeString(value)
	if e != nil || len(b) < c.NonceSize() {
		return errors.New("invalid encrypted AI record")
	}
	raw, e := c.Open(nil, b[:c.NonceSize()], b[c.NonceSize():], []byte(scope))
	if e != nil {
		return errors.New("invalid encrypted AI record")
	}
	return json.Unmarshal(raw, out)
}
func validServiceURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Host != "" && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") && u.RawQuery == "" && u.Fragment == ""
}

func validJobID(id string) bool { return regexp.MustCompile(`^job-[a-f0-9]{32}$`).MatchString(id) }
