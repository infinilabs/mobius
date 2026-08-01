package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"mobius/internal/service"
	"mobius/internal/tools"
	"os"
	"runtime/debug"
	"strings"
	"sync"
)

type MCPServer struct {
	tools         map[string]MCPToolEntry
	mu            sync.RWMutex
	pgClient      *PGClient
	esClient      *ESClient
	config        *Config
	sessionSecret []byte
}

type MCPToolEntry struct {
	Schema  MCPToolSchema
	Execute func(ctx context.Context, args json.RawMessage, caller MCPCaller) (any, error)
}

type MCPCaller struct {
	AgentID string
	TaskID  string
}

type MCPToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

func NewMCPServer(pg *PGClient, es *ESClient, cfg *Config) *MCPServer {
	s := &MCPServer{
		tools:         make(map[string]MCPToolEntry),
		pgClient:      pg,
		esClient:      es,
		config:        cfg,
		sessionSecret: loadMCPSecret(),
	}
	s.registerCoreTools()
	return s
}

// loadMCPSecret returns the HMAC key used to sign MCP session tokens. It is read
// from MOBIUS_MCP_SECRET when set so tokens survive restarts; otherwise an
// ephemeral key is generated (any previously issued tokens become invalid).
func loadMCPSecret() []byte {
	if s := os.Getenv("MOBIUS_MCP_SECRET"); s != "" {
		return []byte(s)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("mcp: failed to generate session secret: " + err.Error())
	}
	slog.Warn("MOBIUS_MCP_SECRET not set; generated ephemeral MCP session secret (tokens invalid across restarts)")
	return b
}

// MintSession issues a signed token binding a caller to an agent and task. The
// control plane hands this token to a spawned agent; the agent presents it when
// connecting to /mcp. Identity is carried in the token, not in client headers.
func (s *MCPServer) MintSession(agentID, taskID string) string {
	payload, _ := json.Marshal(MCPCaller{AgentID: agentID, TaskID: taskID})
	mac := hmac.New(sha256.New, s.sessionSecret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifySession validates a token's HMAC in constant time and returns the caller
// it encodes. A forged or tampered token yields ok=false.
func (s *MCPServer) verifySession(token string) (MCPCaller, bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return MCPCaller{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return MCPCaller{}, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return MCPCaller{}, false
	}
	mac := hmac.New(sha256.New, s.sessionSecret)
	mac.Write(payload)
	if subtle.ConstantTimeCompare(sig, mac.Sum(nil)) != 1 {
		return MCPCaller{}, false
	}
	var caller MCPCaller
	if json.Unmarshal(payload, &caller) != nil || caller.AgentID == "" {
		return MCPCaller{}, false
	}
	return caller, true
}

func (s *MCPServer) registerCoreTools() {
	type toolBinding struct {
		def     ToolDef
		handler func(ctx context.Context, args json.RawMessage, caller MCPCaller) (any, error)
	}

	bindings := []toolBinding{
		{tools.DelegateTaskToolDef, s.handleDelegateTask},
		{tools.HireEmployeeToolDef, s.handleHireEmployee},
		{tools.SubmitTaskResultToolDef, s.handleSubmitResult},
		{tools.ReviewTaskToolDef, s.handleReviewTask},
		{tools.VerifyDeliverableToolDef, s.handleVerifyDeliverable},
		{tools.ListTeamToolDef, s.handleListTeam},
		{tools.StoreMemoryToolDef, s.handleStoreMemory},
		{tools.ForgetMemoryToolDef, s.handleForgetMemory},
		{tools.WriteFileToolDef, s.handleWriteFile},
		{tools.ReadFileToolDef, s.handleReadFile},
		{tools.SearchAssetsToolDef, s.handleSearchAssets},
		{tools.ListAssetsToolDef, s.handleListAssets},
		{tools.RunCommandToolDef, s.handleRunCommand},
		{tools.ListTasksToolDef, s.handleListTasks},
		{tools.GetTaskToolDef, s.handleGetTask},
		{tools.UpdateTaskToolDef, s.handleUpdateTask},
		{tools.UpdateTaskStatusToolDef, s.handleUpdateTaskStatus},
		{tools.AddTaskCommentToolDef, s.handleAddTaskComment},
		{tools.ListEmployeesToolDef, s.handleListEmployees},
		{tools.GetEmployeeToolDef, s.handleGetEmployee},
		{tools.UpdateEmployeeToolDef, s.handleUpdateEmployee},
		{tools.ListProjectsToolDef, s.handleListProjects},
		{tools.CreateProjectToolDef, s.handleCreateProject},
		{tools.UpdateProjectToolDef, s.handleUpdateProject},
		{tools.ListPromptsToolDef, s.handleListPrompts},
		{tools.CreatePromptToolDef, s.handleCreatePrompt},
		{tools.UpdatePromptToolDef, s.handleUpdatePrompt},
		{tools.DeletePromptToolDef, s.handleDeletePrompt},
		{tools.ListSkillsToolDef, s.handleListSkills},
		{tools.AssignSkillToolDef, s.handleAssignSkill},
		{tools.UnassignSkillToolDef, s.handleUnassignSkill},
		{tools.AskUserToolDef, s.handleAskUser},
		{tools.SuggestTasksToolDef, s.handleSuggestTasks},
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range bindings {
		s.tools[b.def.Name] = MCPToolEntry{
			Schema: MCPToolSchema{
				Name:        b.def.Name,
				Description: b.def.Description,
				InputSchema: b.def.Parameters,
			},
			Execute: b.handler,
		}
	}
}

func (s *MCPServer) HandleMessage(ctx context.Context, msg []byte, caller MCPCaller) ([]byte, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		return s.makeError(nil, -32700, "parse error")
	}

	// A request with no "id" is a JSON-RPC notification: the server must not
	// reply (returning a response with "id":null breaks compliant clients).
	if len(req.ID) == 0 {
		return nil, nil
	}

	var resp JSONRPCResponse
	resp.JSONRPC = "2.0"
	resp.ID = req.ID

	switch req.Method {
	case "initialize":
		// Echo the client's requested protocol version so we negotiate rather
		// than forcing a single hardcoded one; fall back to our default when the
		// client omits it. Our JSON-RPC method surface is stable across the MCP
		// revisions a client is likely to request.
		version := "2025-03-26"
		var initParams struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &initParams) == nil && initParams.ProtocolVersion != "" {
			version = initParams.ProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "mobius", "version": "0.1.0"},
		}

	case "notifications/initialized":
		resp.Result = map[string]any{}

	case "tools/list":
		s.mu.RLock()
		tools := make([]MCPToolSchema, 0, len(s.tools))
		for _, t := range s.tools {
			tools = append(tools, t.Schema)
		}
		s.mu.RUnlock()
		resp.Result = map[string]any{"tools": tools}

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.makeError(req.ID, -32602, "invalid params")
		}
		// Reject malformed argument JSON at the protocol boundary so handlers
		// don't silently receive an empty argument map.
		if len(params.Arguments) > 0 && !json.Valid(params.Arguments) {
			return s.makeError(req.ID, -32602, "invalid tool arguments")
		}

		s.mu.RLock()
		handler, ok := s.tools[params.Name]
		s.mu.RUnlock()
		if !ok {
			return s.makeError(req.ID, -32601, "unknown tool: "+params.Name)
		}

		// Single authorization layer (plan 2.1): object-level access is checked
		// here, before any handler runs, from the same policy table the internal
		// adapter and chat paths use.
		var result any
		var err error
		var panicked bool
		if aerr := service.AuthorizeToolCall(ctx, s.pgClient, caller.AgentID, params.Name, parseArgs(params.Arguments), caller.TaskID); aerr != nil {
			err = aerr
		} else if rerr := service.RateLimitToolCall(caller.AgentID, params.Name); rerr != nil {
			// Per-caller spend cap on paid operations (plan 3.4).
			err = rerr
		} else {
			result, err, panicked = s.invokeTool(ctx, params.Name, handler, params.Arguments, caller)
		}
		if panicked {
			return s.makeError(req.ID, -32603, "internal error executing tool: "+params.Name)
		}
		if err != nil {
			resp.Result = map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "Error: " + err.Error()},
				},
				"isError": true,
			}
		} else {
			resultJSON, merr := json.Marshal(result)
			if merr != nil {
				return s.makeError(req.ID, -32603, "failed to encode tool result")
			}
			resp.Result = map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": string(resultJSON)},
				},
			}
		}

	default:
		return s.makeError(req.ID, -32601, "method not found: "+req.Method)
	}

	return json.Marshal(resp)
}

// invokeTool runs a tool handler with a recover guard so a panic in any one of
// the handlers becomes a JSON-RPC internal error for that single call instead of
// crashing the whole server. panicked is true only when the handler panicked.
func (s *MCPServer) invokeTool(ctx context.Context, name string, h MCPToolEntry, args json.RawMessage, caller MCPCaller) (result any, err error, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("PANIC recovered in MCP handler",
				"tool", name, "panic", r, "stack", string(debug.Stack()))
			panicked = true
		}
	}()
	result, err = h.Execute(ctx, args, caller)
	return
}

func (s *MCPServer) makeError(id any, code int, message string) ([]byte, error) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   map[string]any{"code": code, "message": message},
	}
	return json.Marshal(resp)
}

func parseArgs(raw json.RawMessage) map[string]any {
	var args map[string]any
	if raw != nil {
		json.Unmarshal(raw, &args)
	}
	if args == nil {
		args = make(map[string]any)
	}
	return args
}

func argStr(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}
