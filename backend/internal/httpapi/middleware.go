// Package httpapi is the HTTP-layer home (plan 6.4): auth middleware now;
// the thin domain handlers land here as their infra dependencies
// (config/es/gcs/bq/providers/events) get extracted from main.
package httpapi

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strings"
)

// Auth enforces a shared API token on /api/* routes. The token comes from
// the MOBIUS_API_TOKEN env var or server.api_token in config (env wins). When
// no token is configured, auth is disabled — acceptable only because the
// server binds to loopback by default; main refuses to start on a
// non-loopback address without a token.
type Auth struct {
	token string
}

func NewAuth(cfgToken string) *Auth {
	token := os.Getenv("MOBIUS_API_TOKEN")
	if token == "" {
		token = cfgToken
	}
	return &Auth{token: token}
}

func (a *Auth) Enabled() bool { return a.token != "" }

// middleware gates /api/* behind the shared token. Non-API paths (static
// assets, playable previews) and /api/health pass through. The token is
// accepted from an Authorization: Bearer header (axios calls) or the
// mobius_token cookie — media elements (<img>/<video> with /api/... src)
// cannot set headers, and the frontend sets the cookie SameSite=Strict so a
// cross-site page cannot ride it.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	if !a.Enabled() {
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

func (a *Auth) authorized(r *http.Request) bool {
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

// IsLoopbackHost reports whether the bind host only accepts local connections.
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
