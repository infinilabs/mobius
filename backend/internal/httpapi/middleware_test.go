package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func authProbe(t *testing.T, a *Auth, req *http.Request) int {
	t.Helper()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	w := httptest.NewRecorder()
	a.Middleware(ok).ServeHTTP(w, req)
	return w.Code
}

// The middleware is the only thing standing between the network and every
// /api/* mutation, so each acceptance path (bearer header, cookie) and each
// exemption (health, non-API paths) is pinned here.
func TestAPIAuthMiddleware_TokenRequired(t *testing.T) {
	a := &Auth{token: "secret"}

	tests := []struct {
		name string
		req  func() *http.Request
		want int
	}{
		{"no credentials", func() *http.Request {
			return httptest.NewRequest("GET", "/api/tasks", nil)
		}, http.StatusUnauthorized},
		{"wrong bearer token", func() *http.Request {
			r := httptest.NewRequest("GET", "/api/tasks", nil)
			r.Header.Set("Authorization", "Bearer nope")
			return r
		}, http.StatusUnauthorized},
		{"correct bearer token", func() *http.Request {
			r := httptest.NewRequest("GET", "/api/tasks", nil)
			r.Header.Set("Authorization", "Bearer secret")
			return r
		}, http.StatusOK},
		{"wrong cookie", func() *http.Request {
			r := httptest.NewRequest("GET", "/api/tasks", nil)
			r.AddCookie(&http.Cookie{Name: "mobius_token", Value: "nope"})
			return r
		}, http.StatusUnauthorized},
		{"correct cookie (media src loads)", func() *http.Request {
			r := httptest.NewRequest("GET", "/api/projects/p1/assets/a1/content", nil)
			r.AddCookie(&http.Cookie{Name: "mobius_token", Value: "secret"})
			return r
		}, http.StatusOK},
		{"health is exempt", func() *http.Request {
			return httptest.NewRequest("GET", "/api/health", nil)
		}, http.StatusOK},
		{"static assets are exempt", func() *http.Request {
			return httptest.NewRequest("GET", "/index.html", nil)
		}, http.StatusOK},
		{"playable preview is exempt", func() *http.Request {
			return httptest.NewRequest("GET", "/playable-preview/pl1/index.html", nil)
		}, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := authProbe(t, a, tc.req()); got != tc.want {
				t.Errorf("got status %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAPIAuthMiddleware_DisabledPassesThrough(t *testing.T) {
	a := &Auth{token: ""}
	req := httptest.NewRequest("GET", "/api/tasks", nil)
	if got := authProbe(t, a, req); got != http.StatusOK {
		t.Errorf("auth disabled: got status %d, want 200", got)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"localhost", true},
		{"::1", true},
		{"0.0.0.0", false},
		{"", false},
		{"10.0.0.5", false},
		{"example.com", false},
	}
	for _, tc := range tests {
		if got := IsLoopbackHost(tc.host); got != tc.want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}
