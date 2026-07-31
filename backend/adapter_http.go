package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

type HTTPWebhookAdapter struct {
	httpClient *http.Client
	runs       sync.Map
}

type httpRun struct {
	status RunStatus
	errMsg string
	output string
	mu     sync.Mutex
}

// requirePublicIP is the SSRF guard: webhook destinations must be public
// addresses, never loopback, RFC1918/ULA, link-local (cloud metadata), or
// unspecified/multicast.
func requirePublicIP(ip net.IP) error {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("webhook destination %s is not a public address", ip)
	}
	return nil
}

// ssrfGuardControl enforces requirePublicIP on the address actually dialed,
// after DNS resolution — so a hostname that resolves (or rebinds) to a private
// IP, and any redirect target, is blocked at connect time.
func ssrfGuardControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("webhook dial %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("webhook dial %q: not an IP address", address)
	}
	return requirePublicIP(ip)
}

// validateWebhookURL gives a fast, synchronous error for obviously bad URLs
// (scheme, missing host, private-IP or localhost literals). The dial-time
// Control hook remains the authoritative gate for resolved hostnames.
func validateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid WEBHOOK_URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("WEBHOOK_URL must be http(s), got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("WEBHOOK_URL has no host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("webhook destination %s is not a public address", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		return requirePublicIP(ip)
	}
	return nil
}

func NewHTTPWebhookAdapter() *HTTPWebhookAdapter {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: ssrfGuardControl}
	return &HTTPWebhookAdapter{
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{DialContext: dialer.DialContext},
		},
	}
}

func (a *HTTPWebhookAdapter) Type() AdapterType { return AdapterHTTPWebhook }

func (a *HTTPWebhookAdapter) Start(ctx context.Context, hb HeartbeatContext) (string, error) {
	webhookURL := hb.Env["WEBHOOK_URL"]
	if webhookURL == "" {
		return "", fmt.Errorf("WEBHOOK_URL not set in agent env")
	}
	if err := validateWebhookURL(webhookURL); err != nil {
		return "", err
	}

	runID := generateID()

	payload, _ := json.Marshal(map[string]any{
		"run_id":     runID,
		"task_id":    hb.TaskID,
		"task_title": hb.TaskTitle,
		"task_body":  hb.TaskBody,
		"agent_id":   hb.AgentID,
		"agent_name": hb.AgentName,
	})

	run := &httpRun{status: RunActive}
	a.runs.Store(runID, run)

	// Deliver the webhook asynchronously on a detached context: a slow webhook
	// must not block the dispatch loop, and the run is expected to outlive the
	// caller's request (the external worker reports completion via CompleteRun).
	secret := hb.Env["WEBHOOK_SECRET"]
	go func() {
		reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, "POST", webhookURL, bytes.NewReader(payload))
		if err != nil {
			a.failRun(run, "create webhook request: "+err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}

		resp, err := a.httpClient.Do(req)
		if err != nil {
			a.failRun(run, "webhook delivery failed: "+err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			a.failRun(run, fmt.Sprintf("webhook returned %d: %s", resp.StatusCode, body))
			return
		}
		slog.Info("http webhook delivered", "run_id", runID, "url", webhookURL, "task_id", hb.TaskID)
	}()

	return runID, nil
}

func (a *HTTPWebhookAdapter) failRun(run *httpRun, msg string) {
	run.mu.Lock()
	run.status = RunFailed
	run.errMsg = msg
	run.mu.Unlock()
	slog.Warn("http webhook run failed", "error", msg)
}

func (a *HTTPWebhookAdapter) Observe(_ context.Context, runID string) (RunObservation, error) {
	val, ok := a.runs.Load(runID)
	if !ok {
		return RunObservation{Status: RunCompleted}, nil
	}
	run := val.(*httpRun)
	run.mu.Lock()
	defer run.mu.Unlock()
	return RunObservation{
		Status:       run.status,
		Output:       run.output,
		ErrorMessage: run.errMsg,
	}, nil
}

func (a *HTTPWebhookAdapter) Stop(_ context.Context, runID string) error {
	a.runs.Delete(runID)
	return nil
}

func (a *HTTPWebhookAdapter) CompleteRun(runID, output string, success bool) {
	val, ok := a.runs.Load(runID)
	if !ok {
		return
	}
	run := val.(*httpRun)
	run.mu.Lock()
	defer run.mu.Unlock()
	run.output = output
	if success {
		run.status = RunCompleted
	} else {
		run.status = RunFailed
		run.errMsg = output
	}
}

// CompleteRunHandler is the HTTP callback an external webhook worker POSTs to in
// order to report a run's terminal result. Without it a webhook run never reaches
// a terminal Observe and only resolves when the dispatcher's run-ctx cap expires.
// The server-generated run_id (delivered in the original webhook payload) acts as
// the capability: an unknown run_id is rejected, so only a worker that received
// the payload can complete the run.
func (a *HTTPWebhookAdapter) CompleteRunHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		RunID   string `json:"run_id"`
		Output  string `json:"output"`
		Success bool   `json:"success"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RunID == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if _, ok := a.runs.Load(body.RunID); !ok {
		http.Error(w, "unknown run", http.StatusNotFound)
		return
	}
	a.CompleteRun(body.RunID, body.Output, body.Success)
	w.WriteHeader(http.StatusNoContent)
}
