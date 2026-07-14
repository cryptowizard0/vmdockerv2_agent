package openclaw

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	schema "github.com/cryptowizard0/vmdockerv2_agent/runtime/openclaw/schema"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func TestNewAndApply(t *testing.T) {
	const token = "test-gateway-token"

	createCalled := false
	sendCalled := false
	setModelCalled := false
	gatewayPatchCalled := false
	seenCommand := ""
	seenModel := ""
	seenPatch := map[string]interface{}{}

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "data": "pong"})
		case "/tools/invoke":
			if got := r.Header.Get("x-gateway-token"); got != token {
				t.Fatalf("expected x-gateway-token %q, got %q", token, got)
			}

			var req schema.ToolInvokeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode invoke request failed: %v", err)
			}

			switch req.Tool {
			case DefaultToolCreateSession:
				createCalled = true
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "ok",
					"data":   map[string]interface{}{"sessionId": "sess-1"},
				})
			case DefaultToolSendSession:
				sendCalled = true
				if req.SessionKey != "sess-1" {
					t.Fatalf("expected top-level sessionKey sess-1, got %q", req.SessionKey)
				}
				if req.Args["sessionKey"] != "sess-1" {
					t.Fatalf("expected args.sessionKey sess-1, got %v", req.Args["sessionKey"])
				}
				text, _ := req.Args["message"].(string)
				seenCommand = text

				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "ok",
					"result": map[string]interface{}{"reply": "handled"},
				})
			case DefaultToolSetModel:
				setModelCalled = true
				if req.SessionKey != "sess-1" {
					t.Fatalf("expected top-level sessionKey sess-1, got %q", req.SessionKey)
				}
				if req.Args["sessionKey"] != "sess-1" {
					t.Fatalf("expected args.sessionKey sess-1, got %v", req.Args["sessionKey"])
				}
				seenModel, _ = req.Args["model"].(string)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "ok",
					"data":   "model-updated",
				})
			case DefaultToolGateway:
				gatewayPatchCalled = true
				act, _ := req.Args["action"].(string)
				if act != "config.patch" {
					t.Fatalf("expected action config.patch, got %q", act)
				}
				raw, _ := req.Args["raw"].(string)
				if raw == "" {
					t.Fatalf("expected raw json patch string")
				}
				var patch map[string]interface{}
				if err := json.Unmarshal([]byte(raw), &patch); err != nil {
					t.Fatalf("decode raw patch failed: %v", err)
				}
				seenPatch = patch
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "ok",
					"data":   "patched",
				})
			default:
				http.Error(w, "unsupported tool", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	t.Setenv("OPENCLAW_GATEWAY_URL", gateway.URL)
	t.Setenv("OPENCLAW_TIMEOUT_MS", "1000")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", token)
	t.Setenv("OPENCLAW_SESSION_TITLE", "runtime-test")

	rt, err := New()
	if err != nil {
		t.Fatalf("new openclaw runtime failed: %v", err)
	}
	if rt.sessionID() != "sess-1" {
		t.Fatalf("expected session id sess-1, got %q", rt.sessionID())
	}

	res, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Execute", Sequence: 9}, map[string]string{
		"Command":   "run this",
		"Reference": "9",
	})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if res.Data != "handled" {
		t.Fatalf("expected handled, got %q", res.Data)
	}

	if !createCalled {
		t.Fatalf("expected sessions_create to be called")
	}
	if !sendCalled {
		t.Fatalf("expected sessions_send to be called")
	}
	if seenCommand != "run this" {
		t.Fatalf("expected command run this, got %q", seenCommand)
	}

	// Chat with agent and expose reply in result output
	resChat, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Chat", Sequence: 91}, map[string]string{
		"Command":   "hello agent",
		"Reference": "91",
	})
	if err != nil {
		t.Fatalf("apply chat failed: %v", err)
	}
	if resChat.Data != "handled" {
		t.Fatalf("expected chat handled, got %q", resChat.Data)
	}
	if seenCommand != "hello agent" {
		t.Fatalf("expected chat command hello agent, got %q", seenCommand)
	}
	if resChat.Output == nil {
		t.Fatalf("expected output for chat")
	}
	out, ok := resChat.Output.(map[string]interface{})
	if !ok {
		t.Fatalf("expected output map, got %T", resChat.Output)
	}
	if out["action"] != ActionChat {
		t.Fatalf("expected chat output action %q, got %v", ActionChat, out["action"])
	}
	if out["reply"] != "handled" {
		t.Fatalf("expected chat output reply handled, got %v", out["reply"])
	}

	// Configure model
	res2, err := rt.Apply("target-1", vmmSchema.Meta{Action: "ConfigureModel", Sequence: 10}, map[string]string{
		"model":     "gpt-4o",
		"Reference": "10",
	})
	if err != nil {
		t.Fatalf("apply configure model failed: %v", err)
	}
	if res2.Data == "" {
		t.Fatalf("expected non-empty response for ConfigureModel")
	}
	if !setModelCalled {
		t.Fatalf("expected sessions_set_model to be called")
	}
	if seenModel != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %q", seenModel)
	}

	// Configure telegram channel (persistent)
	res3, err := rt.Apply("target-1", vmmSchema.Meta{Action: "ConfigureTelegram", Sequence: 11}, map[string]string{
		"botToken":       "tg-token-1",
		"dmPolicy":       "pairing",
		"allowFrom":      "@alice,+15551234567",
		"defaultAccount": "default",
		"Reference":      "11",
	})
	if err != nil {
		t.Fatalf("apply configure telegram failed: %v", err)
	}
	if res3.Data == "" {
		t.Fatalf("expected non-empty response for ConfigureTelegram")
	}
	if !gatewayPatchCalled {
		t.Fatalf("expected gateway config.patch to be called")
	}
	channels, ok := seenPatch["channels"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected channels object in patch")
	}
	tg, ok := channels["telegram"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected channels.telegram object in patch")
	}
	if tg["botToken"] != "tg-token-1" {
		t.Fatalf("expected botToken tg-token-1, got %v", tg["botToken"])
	}
	if tg["dmPolicy"] != "pairing" {
		t.Fatalf("expected dmPolicy pairing, got %v", tg["dmPolicy"])
	}
	if tg["defaultAccount"] != "default" {
		t.Fatalf("expected defaultAccount default, got %v", tg["defaultAccount"])
	}
	arr, _ := tg["allowFrom"].([]interface{})
	if len(arr) != 2 {
		t.Fatalf("expected 2 allowFrom entries, got %d", len(arr))
	}
}

func TestApplyConfigAutoBaseHash(t *testing.T) {
	createCalled := false
	configGetCalled := false
	configPatchCalled := false
	seenPatchRaw := ""
	seenBaseHash := ""

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "data": "pong"})
		case "/tools/invoke":
			var req schema.ToolInvokeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode invoke request failed: %v", err)
			}

			switch req.Tool {
			case DefaultToolCreateSession:
				createCalled = true
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "ok",
					"data":   map[string]interface{}{"sessionId": "sess-config-1"},
				})
			case DefaultToolGateway:
				action, _ := req.Args["action"].(string)
				switch action {
				case "config.get":
					configGetCalled = true
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"status": "ok",
						"payload": map[string]interface{}{
							"hash": "cfg-hash-1",
						},
					})
				case "config.patch":
					configPatchCalled = true
					seenPatchRaw, _ = req.Args["raw"].(string)
					seenBaseHash, _ = req.Args["baseHash"].(string)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"status": "ok",
						"data":   "config-updated",
					})
				default:
					http.Error(w, "unsupported gateway action", http.StatusBadRequest)
				}
			default:
				http.Error(w, "unsupported tool", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	t.Setenv("OPENCLAW_GATEWAY_URL", gateway.URL)
	t.Setenv("OPENCLAW_TIMEOUT_MS", "1000")

	rt, err := New()
	if err != nil {
		t.Fatalf("new openclaw runtime failed: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected sessions_create to be called")
	}

	raw := "{\"channels\":{\"telegram\":{\"dmPolicy\":\"pairing\"}}}"
	res, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Config", Sequence: 101}, map[string]string{
		"raw":       raw,
		"Reference": "101",
	})
	if err != nil {
		t.Fatalf("apply config failed: %v", err)
	}
	if out, _ := res.Output.(string); out != "config-updated" {
		t.Fatalf("expected output config-updated, got %q", out)
	}
	if !configGetCalled {
		t.Fatalf("expected gateway config.get to be called")
	}
	if !configPatchCalled {
		t.Fatalf("expected gateway config.patch to be called")
	}
	if seenPatchRaw != raw {
		t.Fatalf("expected raw patch %q, got %q", raw, seenPatchRaw)
	}
	if seenBaseHash != "cfg-hash-1" {
		t.Fatalf("expected baseHash cfg-hash-1, got %q", seenBaseHash)
	}
}

func TestNewRestoredUsesCheckpointSessionID(t *testing.T) {
	createCalled := false
	sendSessionKey := ""

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "data": "pong"})
		case "/tools/invoke":
			var req schema.ToolInvokeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode invoke request failed: %v", err)
			}
			switch req.Tool {
			case DefaultToolCreateSession:
				createCalled = true
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "ok",
					"data":   map[string]interface{}{"sessionId": "new-session"},
				})
			case DefaultToolSendSession:
				sendSessionKey = req.SessionKey
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "ok",
					"data":   "restored-ok",
				})
			default:
				http.Error(w, "unsupported tool", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	t.Setenv("OPENCLAW_GATEWAY_URL", gateway.URL)
	t.Setenv("OPENCLAW_TIMEOUT_MS", "1000")

	rt, err := NewRestored(`{"format":"openclaw.runtime.v1","sessionId":"sess-restored-2"}`)
	if err != nil {
		t.Fatalf("new restored openclaw runtime failed: %v", err)
	}
	if rt.sessionID() != "sess-restored-2" {
		t.Fatalf("expected restored session id sess-restored-2, got %q", rt.sessionID())
	}

	_, err = rt.Apply("target-1", vmmSchema.Meta{Action: "Execute", Sequence: 1}, map[string]string{
		"Command": "run restored",
	})
	if err != nil {
		t.Fatalf("apply after restore failed: %v", err)
	}
	if createCalled {
		t.Fatalf("did not expect sessions_create during restored runtime init")
	}
	if sendSessionKey != "sess-restored-2" {
		t.Fatalf("expected sessions_send to use restored session key, got %q", sendSessionKey)
	}

	checkpoint, err := rt.Checkpoint()
	if err != nil {
		t.Fatalf("checkpoint failed: %v", err)
	}
	if !strings.Contains(checkpoint, `"sessionId":"sess-restored-2"`) {
		t.Fatalf("expected checkpoint to include restored session id, got %s", checkpoint)
	}
}

func TestNewRestoredRejectsEmptyCheckpointState(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "data": "pong"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	t.Setenv("OPENCLAW_GATEWAY_URL", gateway.URL)
	t.Setenv("OPENCLAW_TIMEOUT_MS", "1000")

	if _, err := NewRestored(""); err == nil {
		t.Fatalf("expected empty checkpoint state to fail")
	}
}

func TestCheckpointRejectsEmptySessionID(t *testing.T) {
	rt := &Openclaw{}

	if _, err := rt.Checkpoint(); err == nil {
		t.Fatalf("expected checkpoint without session id to fail")
	}
}

func TestApplyApprovePairing(t *testing.T) {
	createCalled := false
	approveCalled := false
	seenChannel := ""
	seenCode := ""

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "data": "pong"})
		case "/tools/invoke":
			var req schema.ToolInvokeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode invoke request failed: %v", err)
			}

			switch req.Tool {
			case DefaultToolCreateSession:
				createCalled = true
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "ok",
					"data":   map[string]interface{}{"sessionId": "sess-pair-1"},
				})
			case DefaultToolGateway:
				action, _ := req.Args["action"].(string)
				if action != "pairing.approve" {
					http.Error(w, "unsupported gateway action", http.StatusBadRequest)
					return
				}
				approveCalled = true
				seenChannel, _ = req.Args["channel"].(string)
				seenCode, _ = req.Args["code"].(string)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"ok":   true,
					"data": "approved",
				})
			default:
				http.Error(w, "unsupported tool", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	t.Setenv("OPENCLAW_GATEWAY_URL", gateway.URL)
	t.Setenv("OPENCLAW_TIMEOUT_MS", "1000")

	rt, err := New()
	if err != nil {
		t.Fatalf("new openclaw runtime failed: %v", err)
	}
	if !createCalled {
		t.Fatalf("expected sessions_create to be called")
	}

	res, err := rt.Apply("target-1", vmmSchema.Meta{Action: "ApproveTelegramPairing", Sequence: 301}, map[string]string{
		"Code":      "FLCSBGVH",
		"Reference": "301",
	})
	if err != nil {
		t.Fatalf("apply approve pairing failed: %v", err)
	}
	if res.Output == nil {
		t.Fatalf("expected non-empty output")
	}
	if !approveCalled {
		t.Fatalf("expected pairing.approve to be called")
	}
	if seenChannel != "telegram" {
		t.Fatalf("expected channel telegram, got %q", seenChannel)
	}
	if seenCode != "FLCSBGVH" {
		t.Fatalf("expected code FLCSBGVH, got %q", seenCode)
	}
}
