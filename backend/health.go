package main

import (
	"context"
	"sync"
	"time"
)

type ServiceStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func StatusOK() ServiceStatus                    { return ServiceStatus{Status: "ok"} }
func StatusUnavailable(err string) ServiceStatus  { return ServiceStatus{Status: "unavailable", Error: err} }
func StatusUnconfigured(err string) ServiceStatus { return ServiceStatus{Status: "unconfigured", Error: err} }

type Probe struct {
	Name  string
	Check func(ctx context.Context) ServiceStatus
}

type HealthChecker struct {
	mu      sync.RWMutex
	probes  []Probe
	timeout time.Duration
}

func NewHealthChecker(timeout time.Duration) *HealthChecker {
	return &HealthChecker{timeout: timeout}
}

func (hc *HealthChecker) Register(name string, check func(ctx context.Context) ServiceStatus) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.probes = append(hc.probes, Probe{Name: name, Check: check})
}

func (hc *HealthChecker) RunAll(parent context.Context) map[string]ServiceStatus {
	hc.mu.RLock()
	probes := make([]Probe, len(hc.probes))
	copy(probes, hc.probes)
	hc.mu.RUnlock()

	ctx, cancel := context.WithTimeout(parent, hc.timeout)
	defer cancel()

	type result struct {
		name string
		ss   ServiceStatus
	}

	ch := make(chan result, len(probes))
	for _, p := range probes {
		go func(p Probe) {
			ch <- result{p.Name, p.Check(ctx)}
		}(p)
	}

	out := make(map[string]ServiceStatus, len(probes))
	for range probes {
		r := <-ch
		out[r.name] = r.ss
	}
	return out
}
