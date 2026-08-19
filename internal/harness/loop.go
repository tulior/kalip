package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Loop runs the agent loop until the model signals done or the
// context is cancelled. Each iteration:
//
//  1. send history to model
//  2. if model returns tool calls, dispatch them and append results
//  3. if model returns a final message, return it
type Loop struct {
	Sh    *Sh
	Model *Model
	Work  string // workspace path
}

// Run executes the task under the static system prompt and a
// user turn that includes the goal (if any).
func (l *Loop) Run(ctx context.Context, task string) error {
	goal, err := LoadGoal(l.Work)
	if err != nil {
		return err
	}

	sys := SystemPrompt
	if goal != "" {
		sys = sys + "\n\nYour current goal (from GOAL.md):\n\n" + goal
	}

	userContent := task
	if l.Work != "" {
		wd, _ := os.Getwd()
		userContent = fmt.Sprintf("Workspace: %s\n\nTask: %s", wd, task)
	}

	msgs := []chatMessage{
		{Role: "system", Content: sys},
		{Role: "user", Content: userContent},
	}

	// Hard cap on loop iterations to prevent runaway.
	const maxIters = 200
	for i := 0; i < maxIters; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		reply, err := l.Model.Chat(ctx, msgs)
		if err != nil {
			return fmt.Errorf("chat: %w", err)
		}

		// No tool calls → final reply.
		if len(reply.ToolCalls) == 0 {
			fmt.Println(reply.Content)
			return nil
		}

		// Append the assistant message.
		msgs = append(msgs, reply)

		// Dispatch each tool call.
		for _, tc := range reply.ToolCalls {
			if tc.Function.Name != ToolSH {
				msgs = append(msgs, chatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    fmt.Sprintf(`{"error":"unknown tool %q"}`, tc.Function.Name),
				})
				continue
			}
			var args struct {
				Cmd string `json:"cmd"`
			}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				msgs = append(msgs, chatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       ToolSH,
					Content:    fmt.Sprintf(`{"error":"bad args: %s"}`, err),
				})
				continue
			}
			res, err := l.Sh.Run(ctx, args.Cmd)
			if err != nil {
				msgs = append(msgs, chatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       ToolSH,
					Content:    fmt.Sprintf(`{"error":"%s"}`, err),
				})
				continue
			}
			msgs = append(msgs, chatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       ToolSH,
				Content:    FormatResult(res),
			})
		}
	}
	return fmt.Errorf("loop: max iterations (%d) reached", maxIters)
}
