package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strings"
)

// apiAuth enforces a shared API token on /api/* routes. The token comes from
// the MOBIUS_API_TOKEN env var or server.api_token in config (env wins). When
// no token is configured, auth is disabled — acceptable only because the
// server binds to loopback by default; main refuses to start on a
// non-loopback address without a token.
type apiAuth struct {
	token string
}

func newAPIAuth(cfgToken string) *apiAuth {
	token := os.Getenv("MOBIUS_API_TOKEN")
	if token == "" {
		token = cfgToken
	}
	return &apiAuth{token: token}
}

func (a *apiAuth) enabled() bool { return a.token != "" }

// middleware gates /api/* behind the shared token. Non-API paths (static
// assets, playable previews) and /api/health pass through. The token is
// accepted from an Authorization: Bearer header (axios calls) or the
// mobius_token cookie — media elements (<img>/<video> with /api/... src)
// cannot set headers, and the frontend sets the cookie SameSite=Strict so a
// cross-site page cannot ride it.
func (a *apiAuth) middleware(next http.Handler) http.Handler {
	if !a.enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		if a.authorized(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func (a *apiAuth) authorized(r *http.Request) bool {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return tokenEqual(strings.TrimPrefix(auth, "Bearer "), a.token)
	}
	if c, err := r.Cookie("mobius_token"); err == nil {
		return tokenEqual(c.Value, a.token)
	}
	return false
}

func tokenEqual(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// isLoopbackHost reports whether the bind host only accepts local connections.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
