package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

// ---- parseTokenFilters: query-string → filter struct, with sane defaults ----

func TestParseTokenFilters_Explicit(t *testing.T) {
	r := httptest.NewRequest("GET",
		"/?since=2020-01-01T00:00:00Z&until=2020-02-01T00:00:00Z&model_id=m1&model_id=m2&source=mcp", nil)
	f := parseTokenFilters(r)
	if !f.Since.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("since not parsed: %v", f.Since)
	}
	if len(f.ModelIDs) != 2 || f.ModelIDs[0] != "m1" {
		t.Errorf("model_id multi-value not captured: %v", f.ModelIDs)
	}
	if len(f.Sources) != 1 || f.Sources[0] != "mcp" {
		t.Errorf("source not captured: %v", f.Sources)
	}
}

func TestParseTokenFilters_Defaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	f := parseTokenFilters(r)
	// Default window is the last 7 days.
	if f.Since.IsZero() || f.Until.IsZero() {
		t.Fatal("default time window not applied")
	}
	if f.Until.Sub(f.Since) < 6*24*time.Hour {
		t.Errorf("default window too narrow: %v..%v", f.Since, f.Until)
	}
}
