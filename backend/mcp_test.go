package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// MintSession/verifySession is the entire MCP authentication boundary (C1):
// caller identity is derived from the signed token, never from a client header.
// A freshly minted token must verify and round-trip the exact identity it was
// minted for — otherwise the control plane cannot trust who an agent is.
func TestMCPSession_RoundTrip(t *testing.T) {
	s := &MCPServer{sessionSecret: []byte("test-secret-key-do-not-use")}
	token := s.MintSession("agent-1", "task-9")
	caller, ok := s.verifySession(token)
	if !ok {
		t.Fatal("freshly minted token failed verification")
	}
	if caller.AgentID != "agent-1" || caller.TaskID != "task-9" {
		t.Errorf("identity not preserved: got %+v", caller)
	}
}

// Malformed/garbage tokens must be rejected, not coerced into an empty caller —
// a token that fails to parse must never authenticate.
func TestMCPSession_RejectsMalformed(t *testing.T) {
	s := &MCPServer{sessionSecret: []byte("secret-A")}
	valid := s.MintSession("agent-1", "task-1")

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no separator", "garbage-no-dot"},
		{"bad base64 payload", "!!!.also-bad"},
		{"truncated signature", valid + "x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := s.verifySession(tc.token); ok {
				t.Errorf("expected rejection for %q", tc.name)
			}
		})
	}
}

// The core security property: you cannot take a validly-signed token and attach a
// different identity payload. Pairing agent-2's payload with agent-1's signature
// must fail, or any agent could impersonate any other (the C1 IDOR vector).
func TestMCPSession_PayloadSignatureMismatch(t *testing.T) {
	s := &MCPServer{sessionSecret: []byte("secret")}
	t1 := s.MintSession("agent-1", "")
	t2 := s.MintSession("agent-2", "")
	p1 := strings.SplitN(t1, ".", 2)
	p2 := strings.SplitN(t2, ".", 2)
	forged := p2[0] + "." + p1[1] // agent-2 identity, agent-1 signature
	if _, ok := s.verifySession(forged); ok {
		t.Fatal("forged token (swapped identity payload) was accepted")
	}
}

// A token minted under one secret must not verify under another. An attacker who
// does not know MOBIUS_MCP_SECRET cannot forge a token that authenticates.
func TestMCPSession_WrongSecret(t *testing.T) {
	minter := &MCPServer{sessionSecret: []byte("real-secret")}
	verifier := &MCPServer{sessionSecret: []byte("attacker-guess")}
	token := minter.MintSession("victim", "task-1")
	if _, ok := verifier.verifySession(token); ok {
		t.Fatal("token forged under a different secret was accepted")
	}
}

// checkOrigin is the CSWSH defense (C2): browsers always send Origin, so a
// cross-site page must be rejected while same-host / localhost / no-Origin
// (non-browser agents) are allowed.
func TestCheckOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{"no origin (non-browser agent)", "", "mobius.local:1983", true},
		{"same host", "http://mobius.local:1983", "mobius.local:1983", true},
		{"localhost dev server", "http://localhost:5173", "mobius.local:1983", true},
		{"loopback ip", "http://127.0.0.1:5173", "mobius.local:1983", true},
		{"cross-site attacker", "https://evil.example.com", "mobius.local:1983", false},
		{"different host", "http://other.host:1983", "mobius.local:1983", false},
		{"malformed origin", "http://%zz", "mobius.local:1983", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Request{Host: tc.host, Header: http.Header{}}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := checkOrigin(r); got != tc.want {
				t.Errorf("checkOrigin(origin=%q host=%q) = %v, want %v",
					tc.origin, tc.host, got, tc.want)
			}
		})
	}
}

// rpcError extracts a JSON-RPC error code from a response, or 0 if there is none.
func rpcError(t *testing.T, resp []byte) int {
	t.Helper()
	var r struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if r.Error == nil {
		return 0
	}
	return r.Error.Code
}

// L15: initialize must echo the client's requested protocol version (negotiation)
// rather than forcing a single hardcoded one.
func TestHandleMessage_InitializeEchoesVersion(t *testing.T) {
	s := &MCPServer{}
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`
	resp, err := s.HandleMessage(context.Background(), []byte(req), MCPCaller{})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &got); err != nil {
		t.Fatal(err)
	}
	if got.Result.ProtocolVersion != "2024-11-05" {
		t.Errorf("initialize did not echo client version: got %q", got.Result.ProtocolVersion)
	}
}

func TestHandleMessage_InitializeDefaultVersion(t *testing.T) {
	s := &MCPServer{}
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	resp, _ := s.HandleMessage(context.Background(), []byte(req), MCPCaller{})
	var got struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	json.Unmarshal(resp, &got)
	if got.Result.ProtocolVersion != "2025-03-26" {
		t.Errorf("expected default version, got %q", got.Result.ProtocolVersion)
	}
}

// H7: a JSON-RPC notification (no "id") must produce NO response — replying with
// "id":null breaks spec-compliant clients.
func TestHandleMessage_NotificationNoResponse(t *testing.T) {
	s := &MCPServer{}
	req := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	resp, err := s.HandleMessage(context.Background(), []byte(req), MCPCaller{})
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil {
		t.Errorf("notification got a response: %s", resp)
	}
}

func TestHandleMessage_Errors(t *testing.T) {
	s := &MCPServer{}
	tests := []struct {
		name string
		req  string
		code int
	}{
		{"parse error", `{not json`, -32700},
		{"unknown method", `{"jsonrpc":"2.0","id":1,"method":"does/not/exist"}`, -32601},
		{"unknown tool", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nope","arguments":{}}}`, -32601},
		{"invalid tools/call params", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":"not-an-object"}`, -32602},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := s.HandleMessage(context.Background(), []byte(tc.req), MCPCaller{})
			if err != nil {
				t.Fatal(err)
			}
			if got := rpcError(t, resp); got != tc.code {
				t.Errorf("got error code %d, want %d (resp: %s)", got, tc.code, resp)
			}
		})
	}
}
