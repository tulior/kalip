// kalip is a thin shell wrapper for coding models.
//
// Usage:
//
//	kalip <task-prompt>
//
// The harness spawns a persistent bash, loads GOAL.md if present,
// sends the static system prompt + task to the model, and runs the
// tool loop until the model produces a final reply.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tulior/kalip/internal/harness"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: kalip <task-prompt>")
		os.Exit(2)
	}
	task := os.Args[1]
	work, _ := os.Getwd()

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sh, err := harness.NewSh(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sh.Close()

	m := harness.NewModel()
	if m.Model == "" {
		fmt.Fprintln(os.Stderr, "KALIP_MODEL not set")
		os.Exit(1)
	}

	loop := &harness.Loop{Sh: sh, Model: m, Work: work}
	if err := loop.Run(ctx, task); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
