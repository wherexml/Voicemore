package cleanup

import (
	"encoding/json"
	"errors"
	"strings"
)

var ErrThinkingParams = errors.New("invalid thinking mode or JSON parameters")

// ThinkingParams validates a small JSON object of provider-specific body fields.
// It never accepts credentials or overrides the application's request contract.
func ThinkingParams(mode, raw string) (map[string]any, error) {
	if mode != "" && mode != "server" && mode != "deepseek" && mode != "omlx" && mode != "custom" {
		return nil, ErrThinkingParams
	}
	if len(raw) > 16384 {
		return nil, ErrThinkingParams
	}
	var params map[string]any
	if strings.TrimSpace(raw) != "" {
		if len(raw) > 16384 || json.Unmarshal([]byte(raw), &params) != nil || params == nil {
			return nil, ErrThinkingParams
		}
		for k, v := range params {
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "model", "messages", "stream", "stream_options", "temperature", "tools", "tool_choice", "functions", "function_call", "input", "prompt", "headers":
				return nil, ErrThinkingParams
			}
			if sensitiveParam(k, v) {
				return nil, ErrThinkingParams
			}
		}
	}
	switch mode {
	case "", "server":
		return nil, nil
	case "deepseek":
		return map[string]any{"thinking": map[string]string{"type": "disabled"}}, nil
	case "omlx":
		return map[string]any{"enable_thinking": false}, nil
	case "custom":
		if len(params) == 0 {
			return nil, ErrThinkingParams
		}
		return params, nil
	}
	return nil, ErrThinkingParams
}
func sensitiveParam(k string, v any) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(k))
	switch normalized {
	case "apikey", "authorization", "password", "secret", "token", "accesstoken", "credential", "credentials", "cookie":
		return true
	}
	switch x := v.(type) {
	case map[string]any:
		for key, value := range x {
			if sensitiveParam(key, value) {
				return true
			}
		}
	case []any:
		for _, value := range x {
			if sensitiveParam("", value) {
				return true
			}
		}
	}
	return false
}
