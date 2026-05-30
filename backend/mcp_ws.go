package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsReadLimit  = 1 << 20 // 1 MiB — reject oversized frames (OOM guard)
	wsPongWait   = 60 * time.Second
	wsPingPeriod = (wsPongWait * 9) / 10
	wsWriteWait  = 10 * time.Second
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: checkOrigin,
}

// checkOrigin blocks cross-site WebSocket hijacking. Non-browser clients (the
// claude CLI, Go agents) send no Origin header and are allowed; browsers always
// send Origin, so a same-host page passes while an attacker's page is rejected.
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return strings.EqualFold(u.Host, r.Host)
}

// wsConn serializes writes. The read/dispatch loop and any future keepalive
// must not write concurrently to a gorilla connection (that panics).
type wsConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *wsConn) write(msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return c.conn.WriteMessage(websocket.TextMessage, msg)
}

func (c *wsConn) ping() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return c.conn.WriteMessage(websocket.PingMessage, nil)
}

func mcpWebSocketHandler(mcp *MCPServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, err := authenticateMCPCaller(mcp, r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("MCP WebSocket upgrade failed", "error", err)
			return
		}
		defer conn.Close()
		wc := &wsConn{conn: conn}

		conn.SetReadLimit(wsReadLimit)
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(wsPongWait))
			return nil
		})

		// Keepalive: ping on a ticker so half-open connections are detected and
		// reaped instead of leaking a goroutine + FD. Pings go through the write
		// mutex; the ticker stops when the read loop returns.
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(wsPingPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := wc.ping(); err != nil {
						return
					}
				case <-done:
					return
				}
			}
		}()

		slog.Info("MCP WebSocket connected", "agent_id", caller.AgentID, "task_id", caller.TaskID)

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					slog.Warn("MCP WebSocket closed unexpectedly", "error", err)
				}
				return
			}

			resp, err := mcp.HandleMessage(r.Context(), msg, caller)
			if err != nil {
				slog.Error("MCP HandleMessage error", "error", err)
				return
			}
			if resp == nil {
				continue // JSON-RPC notification: no response to send
			}

			if err := wc.write(resp); err != nil {
				slog.Warn("MCP WebSocket write failed", "error", err)
				return
			}
		}
	})
}

// authenticateMCPCaller derives the caller's identity from a signed session
// token only. The token is accepted from the Authorization header or the
// ?token= query param (not from a cookie, so a cookie alone cannot authorize a
// cross-site connection). AgentID/TaskID come from the verified token, never
// from client-supplied headers.
func authenticateMCPCaller(mcp *MCPServer, r *http.Request) (MCPCaller, error) {
	var token string
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		token = strings.TrimPrefix(auth, "Bearer ")
	} else if t := r.URL.Query().Get("token"); t != "" {
		token = t
	}

	if token == "" {
		return MCPCaller{}, fmt.Errorf("missing token")
	}

	caller, ok := mcp.verifySession(token)
	if !ok {
		return MCPCaller{}, fmt.Errorf("invalid token")
	}
	return caller, nil
}
