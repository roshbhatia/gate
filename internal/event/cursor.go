package event

import (
	"encoding/json"
	"fmt"

	"github.com/roshbhatia/gate/pkg/gate"
)

// cursorPayload is Cursor's hook input. Every event carries the common
// identity fields; the rest are read by the event that sends them.
type cursorPayload struct {
	HookEventName  string          `json:"hook_event_name"`
	ConversationID string          `json:"conversation_id"`
	SessionID      string          `json:"session_id"`
	WorkspaceRoots []string        `json:"workspace_roots"`
	Cwd            string          `json:"cwd"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolOutput     json.RawMessage `json:"tool_output"`
	MCPServerName  string          `json:"mcp_server_name"`
	ResultJSON     json.RawMessage `json:"result_json"`
	Command        string          `json:"command"`
	Output         string          `json:"output"`
	FilePath       string          `json:"file_path"`
	Edits          json.RawMessage `json:"edits"`
	Prompt         string          `json:"prompt"`
	LoopCount      int             `json:"loop_count"`
	SubagentID     string          `json:"subagent_id"`
	SubagentType   string          `json:"subagent_type"`
	Trigger        string          `json:"trigger"`
}

// cursorEvents maps Cursor's camelCase hook names onto Known. The shell,
// file, and MCP hooks are tool events with the tool implied by the hook.
//
// Left out on purpose: postToolUseFailure (gate has no event for a call that
// did not run), afterAgentResponse and afterAgentThought (observation of the
// reply, which Stop already covers), and the Tab and workspaceOpen hooks,
// which fire outside the agent loop.
var cursorEvents = map[string]string{
	"sessionStart":         "SessionStart",
	"sessionEnd":           "SessionEnd",
	"beforeSubmitPrompt":   "UserPromptSubmit",
	"preToolUse":           "PreToolUse",
	"postToolUse":          "PostToolUse",
	"beforeShellExecution": "PreToolUse",
	"afterShellExecution":  "PostToolUse",
	"beforeMCPExecution":   "PreToolUse",
	"afterMCPExecution":    "PostToolUse",
	"beforeReadFile":       "PreToolUse",
	"afterFileEdit":        "PostToolUse",
	"stop":                 "Stop",
	"subagentStart":        "SubagentStart",
	"subagentStop":         "SubagentStop",
	"preCompact":           "PreCompact",
}

// cursorTools renames Cursor's tools to the names the chains match on.
var cursorTools = map[string]string{"Shell": "Bash"}

// cursorRewriteEvents are the Cursor hooks whose response accepts
// updated_input. beforeShellExecution does not, so a rewrite there needs
// on_rewrite_unsupported.
var cursorRewriteEvents = map[string]bool{"preToolUse": true}

func normalizeCursor(event string, raw []byte) (gate.Envelope, error) {
	var payload cursorPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return gate.Envelope{}, fmt.Errorf("decode cursor hook payload: %w", err)
		}
	}
	if event == "" {
		var ok bool
		if event, ok = cursorEvents[payload.HookEventName]; !ok {
			if payload.HookEventName == "" {
				return gate.Envelope{}, fmt.Errorf("the cursor payload names no event and --event was not given")
			}
			return gate.Envelope{}, fmt.Errorf("cursor event %q has no gate event", payload.HookEventName)
		}
	}
	env := gate.Envelope{
		Version:    gate.Version,
		Harness:    "cursor",
		Event:      event,
		Session:    payload.ConversationID,
		Agent:      gate.Agent{ID: payload.SubagentID, Type: payload.SubagentType},
		Cwd:        payload.Cwd,
		StopActive: payload.LoopCount > 0,
		Prompt:     payload.Prompt,
		Source:     payload.Trigger,
		Raw:        json.RawMessage(raw),
	}
	if env.Session == "" {
		env.Session = payload.SessionID
	}
	// Only the tool hooks carry cwd; the rest have the workspace roots.
	if env.Cwd == "" && len(payload.WorkspaceRoots) > 0 {
		env.Cwd = payload.WorkspaceRoots[0]
	}
	switch payload.HookEventName {
	case "preToolUse", "postToolUse":
		env.Tool = cursorTool(payload.ToolName)
		env.Input = decodeInput(payload.ToolInput)
		env.Response = payload.ToolOutput
	case "beforeShellExecution", "afterShellExecution":
		env.Tool = "Bash"
		env.Input = map[string]any{"command": payload.Command}
		if payload.HookEventName == "afterShellExecution" {
			env.Response, _ = json.Marshal(map[string]string{"output": payload.Output})
		}
	case "beforeMCPExecution", "afterMCPExecution":
		env.Tool = "mcp__" + payload.MCPServerName + "__" + payload.ToolName
		env.Input = decodeInput(payload.ToolInput)
		env.Response = payload.ResultJSON
	case "beforeReadFile":
		env.Tool = "Read"
		env.Input = map[string]any{"file_path": payload.FilePath}
	case "afterFileEdit":
		env.Tool = "Edit"
		env.Input = map[string]any{"file_path": payload.FilePath}
		if len(payload.Edits) > 0 {
			var edits any
			if err := json.Unmarshal(payload.Edits, &edits); err == nil {
				env.Input["edits"] = edits
			}
		}
	}
	return env, nil
}

func cursorTool(name string) string {
	if renamed, ok := cursorTools[name]; ok {
		return renamed
	}
	return name
}

// decodeInput reads tool_input, which is an object on the tool hooks and a
// JSON string on the MCP hooks.
func decodeInput(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err == nil {
		return input
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil
	}
	if err := json.Unmarshal([]byte(encoded), &input); err != nil {
		return nil
	}
	return input
}

// CarriesRewrite reports whether the harness step that sent env can take a
// rewritten input back. Claude, codex, and the gate envelope always can.
func CarriesRewrite(env gate.Envelope) bool {
	if env.Harness != "cursor" {
		return true
	}
	var payload struct {
		HookEventName string `json:"hook_event_name"`
	}
	if err := json.Unmarshal(env.Raw, &payload); err != nil {
		return false
	}
	return cursorRewriteEvents[payload.HookEventName]
}
