package assistance

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Explicit administrator action only: probes can incur provider fees.
func (s *Service) TestProvider(ctx context.Context, draft Draft, user uint, org string) (map[string]string, error) {
	_, old, err := s.configuredRecord()
	if err != nil {
		return nil, err
	}
	if draft.ClearKey {
		return nil, errors.New("cannot test a cleared key")
	}
	draft.BaseURL = strings.TrimSpace(draft.BaseURL)
	if draft.BaseURL != "" && !strings.HasPrefix(draft.BaseURL, "http://") && !strings.HasPrefix(draft.BaseURL, "https://") {
		draft.BaseURL = "http://" + draft.BaseURL
	}
	if draft.APIKey == "" {
		draft.APIKey = old.APIKey
	}
	if draft.APIKey == "" && (strings.HasPrefix(draft.BaseURL, "http://") || strings.Contains(draft.BaseURL, "localhost") || strings.Contains(draft.BaseURL, "127.0.0.1")) {
		draft.APIKey = "none"
	}
	if draft.APIKey == "" || draft.BaseURL == "" || draft.Model == "" {
		return nil, errors.New("complete model configuration required")
	}
	if draft.Protocol != "openai_chat" && draft.Protocol != "openai_responses" {
		return nil, errors.New("unsupported protocol")
	}
	if err = s.DB.Create(&SettingsEvent{UserID: user, OrgID: org, Reason: "模型能力测试：" + draft.Reason}).Error; err != nil {
		return nil, err
	}
	probe, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	var result struct {
		Capabilities map[string]string `json:"capabilities"`
	}
	_, err = s.Client.Call(probe, "POST", "/v2/providers/test", "", map[string]any{"base_url": draft.BaseURL, "api_key": draft.APIKey, "model": draft.Model, "protocol": draft.Protocol}, &result)
	if err != nil {
		return nil, errors.New("model capability probe unavailable")
	}
	states := map[string]string{}
	for _, name := range []string{"connectivity", "structured_output", "streaming", "usage_reporting", "native_web_search", "function_calling", "search_citations"} {
		value := result.Capabilities[name]
		if value != "supported" && value != "unsupported" {
			value = "unknown"
		}
		states[name] = value
	}
	return states, nil
}
