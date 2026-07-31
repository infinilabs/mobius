package main

import (
	"context"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
)

func mustParseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("bad test IP %q", s)
	}
	return ip
}

func TestRequirePublicIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1",       // loopback
		"::1",             // IPv6 loopback
		"10.1.2.3",        // RFC1918
		"172.16.0.1",      // RFC1918
		"192.168.1.1",     // RFC1918
		"169.254.169.254", // link-local (cloud metadata)
		"fe80::1",         // IPv6 link-local
		"fd00::1",         // IPv6 ULA
		"0.0.0.0",         // unspecified (connects to localhost on Linux)
		"::",              // IPv6 unspecified
		"224.0.0.1",       // multicast
	}
	for _, s := range blocked {
		if err := requirePublicIP(mustParseIP(t, s)); err == nil {
			t.Errorf("requirePublicIP(%s) must be refused", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"}
	for _, s := range allowed {
		if err := requirePublicIP(mustParseIP(t, s)); err != nil {
			t.Errorf("requirePublicIP(%s) should be allowed: %v", s, err)
		}
	}
}

func TestValidateWebhookURL(t *testing.T) {
	bad := []string{
		"",
		"file:///etc/passwd",
		"gopher://example.com",
		"http://",                      // no host
		"http://127.0.0.1:8080/hook",   // loopback literal
		"http://169.254.169.254/token", // metadata literal
		"http://localhost:5432/x",      // loopback by name
		"https://[::1]/x",              // IPv6 loopback literal
	}
	for _, u := range bad {
		if err := validateWebhookURL(u); err == nil {
			t.Errorf("validateWebhookURL(%q) must be refused", u)
		}
	}
	good := []string{"https://hooks.example.com/x", "http://example.com:8080/hook"}
	for _, u := range good {
		if err := validateWebhookURL(u); err != nil {
			t.Errorf("validateWebhookURL(%q) should be allowed: %v", u, err)
		}
	}
}

// The adapter's HTTP client must refuse to connect to non-public addresses at
// dial time, so even a hostname that resolves to a private IP (DNS rebinding)
// or a redirect cannot reach internal services.
func TestHTTPWebhookAdapter_ClientBlocksLoopback(t *testing.T) {
	ts := httptest.NewServer(nil)
	defer ts.Close()

	a := NewHTTPWebhookAdapter()
	resp, err := a.httpClient.Get(ts.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatalf("adapter client reached loopback server %s; SSRF guard missing", ts.URL)
	}
	if !strings.Contains(err.Error(), "not a public address") {
		t.Errorf("expected SSRF guard error, got: %v", err)
	}
}

func TestHTTPWebhookAdapter_StartRejectsBadURL(t *testing.T) {
	a := NewHTTPWebhookAdapter()
	_, err := a.Start(context.Background(), HeartbeatContext{
		Env: map[string]string{"WEBHOOK_URL": "http://127.0.0.1:9999/hook"},
	})
	if err == nil {
		t.Fatal("Start must reject a loopback webhook URL")
	}
}
