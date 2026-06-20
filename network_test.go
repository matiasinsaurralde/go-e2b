package e2b

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNetworkConfigSerialization(t *testing.T) {
	t.Run("full config", func(t *testing.T) {
		cfg := NetworkConfig{
			AllowOut: []string{"api.example.com", "1.1.1.1"},
			DenyOut:  []string{AllTraffic},
			Rules: map[string][]RequestRule{
				"api.example.com": {
					{Transform: RequestTransform{Headers: map[string]string{"Authorization": "Bearer tok"}}},
				},
			},
			EgressProxy: &EgressProxyConfig{
				Address:  "proxy.example.com:1080",
				Username: "user",
				Password: "pass",
			},
			AllowPublicTraffic: Bool(false),
			MaskRequestHost:    "custom.example.com:${PORT}",
		}

		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		// Check field names match API expectations
		if _, ok := m["allowOut"]; !ok {
			t.Error("missing allowOut")
		}
		if _, ok := m["denyOut"]; !ok {
			t.Error("missing denyOut")
		}
		if _, ok := m["rules"]; !ok {
			t.Error("missing rules")
		}
		if _, ok := m["egressProxy"]; !ok {
			t.Error("missing egressProxy")
		}
		if _, ok := m["allowPublicTraffic"]; !ok {
			t.Error("missing allowPublicTraffic")
		}
		if v, ok := m["allowPublicTraffic"].(bool); !ok || v != false {
			t.Errorf("allowPublicTraffic = %v, want false", m["allowPublicTraffic"])
		}
		if v, ok := m["maskRequestHost"].(string); !ok || v != "custom.example.com:${PORT}" {
			t.Errorf("maskRequestHost = %v, want string", m["maskRequestHost"])
		}
	})

	t.Run("nil fields omitted", func(t *testing.T) {
		cfg := NetworkConfig{}
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(data) != "{}" {
			t.Errorf("empty config = %s, want {}", string(data))
		}
	})

	t.Run("allowPublicTraffic nil is omitted", func(t *testing.T) {
		cfg := NetworkConfig{AllowOut: []string{"1.1.1.1"}}
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if _, ok := m["allowPublicTraffic"]; ok {
			t.Error("nil AllowPublicTraffic should be omitted")
		}
	})
}

func TestNetworkUpdateConfigSerialization(t *testing.T) {
	t.Run("allow_internet_access uses snake_case", func(t *testing.T) {
		cfg := NetworkUpdateConfig{
			AllowInternetAccess: Bool(false),
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if _, ok := m["allow_internet_access"]; !ok {
			t.Error("missing allow_internet_access (snake_case)")
		}
		if _, ok := m["allowInternetAccess"]; ok {
			t.Error("should not use camelCase allowInternetAccess")
		}
	})

	t.Run("empty config serializes to {}", func(t *testing.T) {
		cfg := NetworkUpdateConfig{}
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(data) != "{}" {
			t.Errorf("empty update config = %s, want {}", string(data))
		}
	})

	t.Run("with rules", func(t *testing.T) {
		cfg := NetworkUpdateConfig{
			AllowOut: []string{"api.example.com"},
			DenyOut:  []string{AllTraffic},
			Rules: map[string][]RequestRule{
				"api.example.com": {
					{Transform: RequestTransform{Headers: map[string]string{"X-Key": "val"}}},
				},
			},
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if _, ok := m["rules"]; !ok {
			t.Error("missing rules")
		}
	})
}

func TestEgressProxyConfigSerialization(t *testing.T) {
	t.Run("with auth", func(t *testing.T) {
		cfg := EgressProxyConfig{
			Address:  "proxy:1080",
			Username: "u",
			Password: "p",
		}
		data, _ := json.Marshal(cfg)
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if m["address"] != "proxy:1080" {
			t.Errorf("address = %v", m["address"])
		}
		if m["username"] != "u" {
			t.Errorf("username = %v", m["username"])
		}
	})

	t.Run("without auth", func(t *testing.T) {
		cfg := EgressProxyConfig{Address: "proxy:1080"}
		data, _ := json.Marshal(cfg)
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if _, ok := m["username"]; ok {
			t.Error("username should be omitted")
		}
		if _, ok := m["password"]; ok {
			t.Error("password should be omitted")
		}
	})
}

func TestDenyAllOutbound(t *testing.T) {
	cfg := DenyAllOutbound()
	if len(cfg.DenyOut) != 1 || cfg.DenyOut[0] != AllTraffic {
		t.Errorf("DenyOut = %v, want [%s]", cfg.DenyOut, AllTraffic)
	}
	if cfg.AllowOut != nil {
		t.Errorf("AllowOut = %v, want nil", cfg.AllowOut)
	}
}

func TestAllowOutbound(t *testing.T) {
	cfg := AllowOutbound("api.example.com", "1.1.1.1")
	if len(cfg.AllowOut) != 2 {
		t.Fatalf("AllowOut len = %d, want 2", len(cfg.AllowOut))
	}
	if cfg.AllowOut[0] != "api.example.com" {
		t.Errorf("AllowOut[0] = %q", cfg.AllowOut[0])
	}
	if len(cfg.DenyOut) != 1 || cfg.DenyOut[0] != AllTraffic {
		t.Errorf("DenyOut = %v, want [%s]", cfg.DenyOut, AllTraffic)
	}
}

func TestWithRequestTransform(t *testing.T) {
	cfg := AllowOutbound("api.example.com").
		WithRequestTransform("api.example.com", map[string]string{"Authorization": "Bearer tok"})

	rules, ok := cfg.Rules["api.example.com"]
	if !ok {
		t.Fatal("missing rules for api.example.com")
	}
	if len(rules) != 1 {
		t.Fatalf("rules len = %d, want 1", len(rules))
	}
	if rules[0].Transform.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("Authorization header = %q", rules[0].Transform.Headers["Authorization"])
	}
}

func TestWithRequestTransformAppends(t *testing.T) {
	cfg := AllowOutbound("a.com", "b.com").
		WithRequestTransform("a.com", map[string]string{"X-A": "1"}).
		WithRequestTransform("b.com", map[string]string{"X-B": "2"}).
		WithRequestTransform("a.com", map[string]string{"X-A2": "3"})

	if len(cfg.Rules["a.com"]) != 2 {
		t.Errorf("rules for a.com = %d, want 2", len(cfg.Rules["a.com"]))
	}
	if len(cfg.Rules["b.com"]) != 1 {
		t.Errorf("rules for b.com = %d, want 1", len(cfg.Rules["b.com"]))
	}
}

func TestBoolHelper(t *testing.T) {
	p := Bool(true)
	if p == nil || *p != true {
		t.Error("Bool(true) failed")
	}
	p = Bool(false)
	if p == nil || *p != false {
		t.Error("Bool(false) failed")
	}
}

// Integration tests with mock server

func TestNewSandboxWithNetworkConfig(t *testing.T) {
	var captured createRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-net",
			EnvdAccessToken: "token",
		})
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	_, err := client.NewSandbox(context.Background(), SandboxConfig{
		Network: AllowOutbound("api.example.com").
			WithRequestTransform("api.example.com", map[string]string{"X-Key": "val"}),
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}

	if captured.Network == nil {
		t.Fatal("network is nil in request")
	}
	if len(captured.Network.AllowOut) != 1 || captured.Network.AllowOut[0] != "api.example.com" {
		t.Errorf("allowOut = %v", captured.Network.AllowOut)
	}
	if len(captured.Network.DenyOut) != 1 || captured.Network.DenyOut[0] != AllTraffic {
		t.Errorf("denyOut = %v", captured.Network.DenyOut)
	}
	rules := captured.Network.Rules["api.example.com"]
	if len(rules) != 1 || rules[0].Transform.Headers["X-Key"] != "val" {
		t.Errorf("rules = %+v", rules)
	}
}

func TestNewSandboxWithoutNetworkConfig(t *testing.T) {
	var captured createRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{SandboxID: "sbx-no-net"})
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	_, err := client.NewSandbox(context.Background())
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}

	if captured.Network != nil {
		t.Errorf("network should be nil, got %+v", captured.Network)
	}
}

func TestNewSandboxSecureWithTrafficToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cr createRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !cr.Secure {
			t.Error("secure should be true")
		}
		if cr.Network == nil || cr.Network.AllowPublicTraffic == nil || *cr.Network.AllowPublicTraffic != false {
			t.Error("allowPublicTraffic should be false")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:          "sbx-secure",
			EnvdAccessToken:    "envd-tok",
			TrafficAccessToken: "traffic-tok-abc",
		})
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		Secure: true,
		Network: &NetworkConfig{
			AllowPublicTraffic: Bool(false),
		},
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	if sbx.TrafficAccessToken != "traffic-tok-abc" {
		t.Errorf("TrafficAccessToken = %q, want %q", sbx.TrafficAccessToken, "traffic-tok-abc")
	}
}

func TestNewSandboxAllowInternetAccess(t *testing.T) {
	var captured createRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{SandboxID: "sbx-inet"})
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	_, err := client.NewSandbox(context.Background(), SandboxConfig{
		AllowInternetAccess: Bool(false),
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	if captured.AllowInternetAccess == nil || *captured.AllowInternetAccess != false {
		t.Error("allow_internet_access should be false")
	}
}

func TestSandboxInfoWithNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"sandboxID": "sbx-info",
			"templateID": "base",
			"state": "running",
			"cpuCount": 2,
			"memoryMB": 512,
			"diskSizeMB": 1024,
			"startedAt": "2024-01-01T00:00:00Z",
			"network": {
				"allowOut": ["api.example.com"],
				"denyOut": ["0.0.0.0/0"],
				"allowPublicTraffic": true,
				"maskRequestHost": "proxy.e2b.app",
				"rules": {
					"api.example.com": [{"transform": {"headers": {"Authorization": "Bearer tok"}}}]
				}
			}
		}`))
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx := &Sandbox{ID: "sbx-info", client: client}
	info, err := sbx.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Network == nil {
		t.Fatal("Network is nil")
	}
	if len(info.Network.AllowOut) != 1 || info.Network.AllowOut[0] != "api.example.com" {
		t.Errorf("AllowOut = %v", info.Network.AllowOut)
	}
	if len(info.Network.DenyOut) != 1 || info.Network.DenyOut[0] != AllTraffic {
		t.Errorf("DenyOut = %v", info.Network.DenyOut)
	}
	if info.Network.AllowPublicTraffic == nil || *info.Network.AllowPublicTraffic != true {
		t.Error("AllowPublicTraffic should be true")
	}
	if info.Network.MaskRequestHost != "proxy.e2b.app" {
		t.Errorf("MaskRequestHost = %q", info.Network.MaskRequestHost)
	}
	rules := info.Network.Rules["api.example.com"]
	if len(rules) != 1 || rules[0].Transform.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("rules = %+v", rules)
	}
}

func TestUpdateNetworkSuccess(t *testing.T) {
	var capturedBody NetworkUpdateConfig
	var capturedMethod, capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx := &Sandbox{ID: "sbx-update", client: client}

	err := sbx.UpdateNetwork(NetworkUpdateConfig{
		AllowOut: []string{"api.example.com", "cdn.example.com"},
		DenyOut:  []string{AllTraffic},
		Rules: map[string][]RequestRule{
			"api.example.com": {
				{Transform: RequestTransform{Headers: map[string]string{"Authorization": "Bearer rt"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateNetwork: %v", err)
	}

	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedPath != "/sandboxes/sbx-update/network" {
		t.Errorf("path = %s", capturedPath)
	}
	if len(capturedBody.AllowOut) != 2 {
		t.Errorf("AllowOut = %v", capturedBody.AllowOut)
	}
	if capturedBody.Rules == nil {
		t.Error("Rules should not be nil")
	}
}

func TestUpdateNetworkEmpty(t *testing.T) {
	var rawBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		rawBody, err = readAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx := &Sandbox{ID: "sbx-clear", client: client}

	err := sbx.UpdateNetwork(NetworkUpdateConfig{})
	if err != nil {
		t.Fatalf("UpdateNetwork: %v", err)
	}
	if string(rawBody) != "{}" {
		t.Errorf("body = %s, want {}", string(rawBody))
	}
}

func TestUpdateNetworkNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"not found"}`))
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx := &Sandbox{ID: "sbx-gone", client: client}

	err := sbx.UpdateNetwork(NetworkUpdateConfig{AllowOut: []string{"1.1.1.1"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var nfe *SandboxNotFoundError
	if !errors.As(err, &nfe) {
		t.Errorf("error type = %T, want *SandboxNotFoundError", err)
	}
}

func TestUpdateNetworkConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":409,"message":"conflict"}`))
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx := &Sandbox{ID: "sbx-conflict", client: client}

	err := sbx.UpdateNetwork(NetworkUpdateConfig{})
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if e.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", e.StatusCode)
	}
}

func TestUpdateNetworkWithAllowInternetAccess(t *testing.T) {
	var captured NetworkUpdateConfig
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx := &Sandbox{ID: "sbx-inet", client: client}

	err := sbx.UpdateNetwork(NetworkUpdateConfig{
		AllowInternetAccess: Bool(false),
	})
	if err != nil {
		t.Fatalf("UpdateNetwork: %v", err)
	}
	if captured.AllowInternetAccess == nil || *captured.AllowInternetAccess != false {
		t.Error("AllowInternetAccess should be false")
	}
}

// readAll reads all bytes from a reader, for test use.
func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 1024)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}
