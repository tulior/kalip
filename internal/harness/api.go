package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Tool name. There is only one.
const ToolSH = "sh"

// toolSH is the JSON schema for the sh tool, sent to the API.
const toolSHSchema = `{
  "type": "function",
  "function": {
    "name": "sh",
    "description": "Execute a Bash command in a persistent shell. cwd, env, and shell state persist across calls. Each call is one new command. Returns {stdout, exit_code, duration, truncated}.",
    "parameters": {
      "type": "object",
      "properties": {
        "cmd": {
          "type": "string",
          "description": "The bash command to execute."
        }
      },
      "required": ["cmd"]
    }
  }
}`

// chatRequest is the OpenAI-compatible chat completions payload.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools"`
	ToolChoice  string        `json:"tool_choice"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// chatResponse is the non-streaming response.
type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Model is the OpenAI-compatible API client.
type Model struct {
	APIKey  string
	APIBase string
	Model   string
	HTTP    *http.Client
}

// NewModel constructs a Model from environment variables.
func NewModel() *Model {
	return &Model{
		APIKey:  os.Getenv("KALIP_API_KEY"),
		APIBase: os.Getenv("KALIP_API_BASE"),
		Model:   os.Getenv("KALIP_MODEL"),
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// Chat sends the message history and returns the assistant's
// reply (which may contain tool calls).
func (m *Model) Chat(ctx context.Context, msgs []chatMessage) (chatMessage, error) {
	if m.APIBase == "" {
		m.APIBase = "https://api.openai.com/v1"
	}

	tools := []chatTool{{Type: "function"}}
	_ = json.Unmarshal([]byte(toolSHSchema), &tools[0].Type)

	// Parse and inject the tool schema properly.
	var sch struct {
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	_ = json.Unmarshal([]byte(toolSHSchema), &sch)
	tools[0].Function = sch.Function

	req := chatRequest{
		Model:       m.Model,
		Messages:    msgs,
		Tools:       tools,
		ToolChoice:  "auto",
		Temperature: 0,
		Stream:      false,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return chatMessage{}, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		m.APIBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatMessage{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if m.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+m.APIKey)
	}

	resp, err := m.HTTP.Do(httpReq)
	if err != nil {
		return chatMessage{}, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return chatMessage{}, fmt.Errorf("api %d: %s", resp.StatusCode, raw)
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return chatMessage{}, fmt.Errorf("unmarshal: %w (body=%s)", err, raw)
	}
	if cr.Error != nil {
		return chatMessage{}, fmt.Errorf("api: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return chatMessage{}, fmt.Errorf("api: no choices")
	}
	return cr.Choices[0].Message, nil
}

// FormatResult renders a ShResult for the model as a JSON string
// suitable for the tool result message.
func FormatResult(r ShResult) string {
	out := struct {
		Stdout       string `json:"stdout"`
		ExitCode     int    `json:"exit_code"`
		DurationMs   int64  `json:"duration_ms"`
		Truncated    bool   `json:"truncated,omitempty"`
		TruncMessage string `json:"trunc_message,omitempty"`
	}{
		Stdout:       r.Stdout,
		ExitCode:     r.ExitCode,
		DurationMs:   r.Duration.Milliseconds(),
		Truncated:    r.Truncated,
		TruncMessage: r.TruncMessage,
	}
	if r.Truncated {
		out.Stdout = r.TruncMessage + r.Stdout
	}
	b, _ := json.Marshal(out)
	return string(b)
}
