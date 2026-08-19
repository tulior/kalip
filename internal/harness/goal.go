package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

// GoalFile is the workspace-relative path the model writes its
// task tracking to. The harness reads it but never writes it.
const GoalFile = "GOAL.md"

// LoadGoal reads GOAL.md from the workspace. Returns empty string
// if the file does not exist. Errors only on permission / I/O
// problems other than ENOENT.
func LoadGoal(workdir string) (string, error) {
	path := filepath.Join(workdir, GoalFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("goal: read %s: %w", GoalFile, err)
	}
	return string(b), nil
}
