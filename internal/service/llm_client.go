package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LLMClient is a minimal OpenAI-compatible chat completion client
type LLMClient struct {
	baseURL              string
	apiKey               string
	model                string
	http                 *http.Client
	allowPrivateEndpoint bool
}

type LLMClientOption func(*LLMClient)

func AllowPrivateLLMEndpoints(allow bool) LLMClientOption {
	return func(c *LLMClient) {
		c.allowPrivateEndpoint = allow
		c.http = llmHTTPClient(allow)
	}
}

// NewLLMClient creates a new LLM client
func NewLLMClient(baseURL, apiKey, model string, opts ...LLMClientOption) *LLMClient {
	c := &LLMClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    llmHTTPClient(false),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// Chat sends a non-streaming chat request and returns the response
func (c *LLMClient) Chat(messages []chatMessage) (string, error) {
	targetURL, err := c.chatCompletionsURL()
	if err != nil {
		return "", err
	}

	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("LLM returned %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", err
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// ChatStream sends a streaming chat request and calls onChunk for each delta
func (c *LLMClient) ChatStream(messages []chatMessage, onChunk func(content string) error) error {
	targetURL, err := c.chatCompletionsURL()
	if err != nil {
		return err
	}

	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("LLM returned %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var delta streamDelta
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			continue
		}

		if len(delta.Choices) > 0 && delta.Choices[0].Delta.Content != "" {
			if err := onChunk(delta.Choices[0].Delta.Content); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}

func (c *LLMClient) chatCompletionsURL() (string, error) {
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		return "", fmt.Errorf("LLM base URL is required")
	}
	targetURL := baseURL + "/chat/completions"
	if err := validateLLMEndpoint(targetURL, c.allowPrivateEndpoint); err != nil {
		return "", err
	}
	return targetURL, nil
}

func validateLLMEndpoint(rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid LLM base URL")
	}
	if u.User != nil {
		return fmt.Errorf("LLM base URL must not include user info")
	}
	if allowPrivate {
		if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
			return fmt.Errorf("LLM base URL must use http or https")
		}
		return nil
	}
	return validatePublicHTTPSLLMEndpointWithResolver(rawURL, net.LookupIP)
}

func validatePublicHTTPSLLMEndpointWithResolver(rawURL string, lookup func(string) ([]net.IP, error)) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid LLM base URL")
	}
	if u.User != nil {
		return fmt.Errorf("LLM base URL must not include user info")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("LLM base URL must use https")
	}

	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("LLM base URL host is not allowed")
	}

	ips, err := lookup(host)
	if err != nil {
		return fmt.Errorf("failed to resolve LLM base URL host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("LLM base URL host did not resolve")
	}
	for _, ip := range ips {
		if !isPublicLLMIP(ip) {
			return fmt.Errorf("LLM base URL must resolve to public IP addresses")
		}
	}
	return nil
}

func isPublicLLMIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return true
}

func llmHTTPClient(allowPrivate bool) *http.Client {
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return validateLLMEndpoint(req.URL.String(), allowPrivate)
		},
	}
	if !allowPrivate {
		client.Transport = &http.Transport{DialContext: llmDialContext}
	}
	return client
}

func llmDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("LLM base URL host did not resolve")
	}
	for _, ip := range ips {
		if !isPublicLLMIP(ip.IP) {
			return nil, fmt.Errorf("LLM base URL must resolve to public IP addresses")
		}
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	for _, ip := range ips {
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
	}
	return nil, fmt.Errorf("LLM base URL must resolve to public IP addresses")
}
