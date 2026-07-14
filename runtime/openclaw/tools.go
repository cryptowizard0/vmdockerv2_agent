package openclaw

import (
	"encoding/json"
	"strings"

	schema "github.com/cryptowizard0/vmdockerv2_agent/runtime/openclaw/schema"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

const (
	DefaultToolCreateSession = "sessions_create"
	DefaultToolSendSession   = "sessions_send"
	DefaultToolCloseSession  = "sessions_delete"
	DefaultToolSetModel      = "session_status"
	DefaultToolGateway       = "gateway"
)

func resolveSendTool(action string) string {
	switch action {
	case ActionQuery:
		return getEnvOrDefault("OPENCLAW_TOOL_QUERY", DefaultToolSendSession)
	case ActionExecute:
		return getEnvOrDefault("OPENCLAW_TOOL_EXECUTE", DefaultToolSendSession)
	case ActionChat:
		return getEnvOrDefault("OPENCLAW_TOOL_CHAT", DefaultToolSendSession)
	default:
		return getEnvOrDefault("OPENCLAW_TOOL_SEND_SESSION", DefaultToolSendSession)
	}
}

func resolveSetModelTool() string {
	return getEnvOrDefault("OPENCLAW_TOOL_SET_MODEL", DefaultToolSetModel)
}

func resolveGatewayTool() string {
	return getEnvOrDefault("OPENCLAW_TOOL_GATEWAY", DefaultToolGateway)
}

func newToolInvokeRequest(tool string, args map[string]interface{}, sessionKey string) schema.ToolInvokeRequest {
	req := schema.ToolInvokeRequest{
		Tool:      tool,
		Args:      args,
		Arguments: args,
	}
	if sessionKey != "" {
		req.SessionKey = sessionKey
	}
	return req
}

func normalizeProviderName(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func splitModelName(model string) (string, string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", ""
	}
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return "", model
	}
	return normalizeProviderName(parts[0]), strings.TrimSpace(parts[1])
}

func extractModelInput(meta vmmSchema.Meta, params map[string]string) string {
	for _, key := range []string{"model", "Model", "modelName", "ModelName"} {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(meta.Data); value != "" {
		return value
	}
	return ""
}

func extractModelName(meta vmmSchema.Meta, params map[string]string) string {
	provider := extractProviderTagValue(params)
	model := extractModelInput(meta, params)
	if model == "" {
		return ""
	}
	if provider == "" {
		return strings.TrimSpace(model)
	}
	_, suffix := splitModelName(model)
	if suffix == "" {
		return provider
	}
	return provider + "/" + suffix
}

func extractProviderName(meta vmmSchema.Meta, params map[string]string) string {
	provider := extractProviderTagValue(params)
	if provider != "" {
		return provider
	}
	model := extractModelName(meta, params)
	modelProvider, _ := splitModelName(model)
	return modelProvider
}

func extractProviderTagValue(params map[string]string) string {
	provider := strings.TrimSpace(params["provider"])
	if provider == "" {
		provider = strings.TrimSpace(params["Provider"])
	}
	return normalizeProviderName(provider)
}

func extractModelAPIKey(params map[string]string) string {
	for _, key := range []string{
		"apiKey", "ApiKey", "APIKey",
		"modelApiKey", "ModelApiKey",
		"KIMI_API_KEY", "KIMICODE_API_KEY", "MOONSHOT_API_KEY",
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY",
		"GEMINI_API_KEY", "GOOGLE_API_KEY",
	} {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	return ""
}

func extractAuthProfileID(provider string, params map[string]string) string {
	for _, key := range []string{"authProfileId", "AuthProfileId", "profileId", "ProfileId"} {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	if provider == "" {
		return ""
	}
	return provider + ":default"
}

func buildTelegramConfigPatch(params map[string]string) map[string]interface{} {
	tg := map[string]interface{}{}
	if v := strings.TrimSpace(params["botToken"]); v != "" {
		tg["botToken"] = v
	}
	if v := strings.TrimSpace(params["defaultAccount"]); v != "" {
		tg["defaultAccount"] = v
	}
	if v := strings.TrimSpace(params["dmPolicy"]); v != "" {
		tg["dmPolicy"] = v
	}
	if v := strings.TrimSpace(params["allowFrom"]); v != "" {
		if list := parseStringList(v); len(list) > 0 {
			tg["allowFrom"] = list
		}
	}
	if len(tg) == 0 {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"channels": map[string]interface{}{
			"telegram": tg,
		},
	}
}

func extractConfigRaw(meta vmmSchema.Meta, params map[string]string) string {
	for _, key := range []string{"raw", "Raw", "patch", "Patch", "config", "Config"} {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(meta.Data); value != "" {
		return value
	}
	return ""
}

func extractConfigAction(params map[string]string) string {
	for _, key := range []string{"configAction", "ConfigAction", "method", "Method"} {
		value := strings.TrimSpace(strings.ToLower(params[key]))
		switch value {
		case "config.patch", "patch":
			return "config.patch"
		case "config.apply", "apply":
			return "config.apply"
		}
	}
	return "config.patch"
}

func extractConfigBaseHash(params map[string]string) string {
	for _, key := range []string{"baseHash", "BaseHash", "hash", "Hash"} {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	return ""
}

func extractPairingChannel(params map[string]string) string {
	for _, key := range []string{"channel", "Channel"} {
		if value := strings.TrimSpace(strings.ToLower(params[key])); value != "" {
			return value
		}
	}
	return "telegram"
}

func extractPairingCode(meta vmmSchema.Meta, params map[string]string) string {
	for _, key := range []string{"code", "Code", "pairingCode", "PairingCode"} {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(meta.Data); value != "" {
		return value
	}
	return ""
}

func parseStringList(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	if strings.HasPrefix(input, "[") && strings.HasSuffix(input, "]") {
		var arr []string
		if err := json.Unmarshal([]byte(input), &arr); err == nil {
			out := make([]string, 0, len(arr))
			for _, s := range arr {
				if ss := strings.TrimSpace(s); ss != "" {
					out = append(out, ss)
				}
			}
			return out
		}
	}
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func extractData(v interface{}) string {
	switch vv := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(vv)
	case map[string]interface{}:
		for _, key := range []string{"data", "result", "message", "text", "reply", "output", "content"} {
			if nested, ok := vv[key]; ok {
				if out := extractData(nested); out != "" {
					return out
				}
			}
		}
		for _, nested := range vv {
			switch nested.(type) {
			case map[string]interface{}, []interface{}:
				if out := extractData(nested); out != "" {
					return out
				}
			}
		}
	case []interface{}:
		for _, nested := range vv {
			if out := extractData(nested); out != "" {
				return out
			}
		}
	}
	return ""
}

func extractSessionID(body map[string]interface{}) string {
	if len(body) == 0 {
		return ""
	}

	for _, path := range [][]string{
		{"sessionId"},
		{"sessionID"},
		{"data", "sessionId"},
		{"data", "sessionID"},
		{"data", "session", "id"},
		{"result", "sessionId"},
		{"result", "sessionID"},
		{"result", "session", "id"},
	} {
		if value := lookupStringPath(body, path...); value != "" {
			return value
		}
	}

	return findSessionIDRecursive(body)
}

func lookupStringPath(root map[string]interface{}, path ...string) string {
	var current interface{} = root
	for _, segment := range path {
		nextMap, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		next, ok := nextMap[segment]
		if !ok {
			return ""
		}
		current = next
	}

	text, ok := current.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func findSessionIDRecursive(v interface{}) string {
	switch vv := v.(type) {
	case map[string]interface{}:
		for key, value := range vv {
			if normalizeKey(key) != "sessionid" {
				continue
			}
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
		for _, value := range vv {
			if out := findSessionIDRecursive(value); out != "" {
				return out
			}
		}
	case []interface{}:
		for _, value := range vv {
			if out := findSessionIDRecursive(value); out != "" {
				return out
			}
		}
	}
	return ""
}

func extractConfigHash(body map[string]interface{}) string {
	if len(body) == 0 {
		return ""
	}
	for _, path := range [][]string{
		{"payload", "hash"},
		{"data", "payload", "hash"},
		{"result", "payload", "hash"},
		{"hash"},
		{"data", "hash"},
		{"result", "hash"},
	} {
		if value := lookupStringPath(body, path...); value != "" {
			return value
		}
	}
	return findConfigHashRecursive(body)
}

func findConfigHashRecursive(v interface{}) string {
	switch vv := v.(type) {
	case map[string]interface{}:
		for key, value := range vv {
			if normalizeKey(key) != "hash" {
				continue
			}
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
		for _, value := range vv {
			if out := findConfigHashRecursive(value); out != "" {
				return out
			}
		}
	case []interface{}:
		for _, value := range vv {
			if out := findConfigHashRecursive(value); out != "" {
				return out
			}
		}
	}
	return ""
}

func normalizeKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	return key
}
