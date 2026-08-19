package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGoalMissing(t *testing.T) {
	dir := t.TempDir()
	g, err := LoadGoal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if g != "" {
		t.Errorf("expected empty, got %q", g)
	}
}

func TestLoadGoalPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, GoalFile),
		[]byte("fix the bug in foo.py"), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGoal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if g != "fix the bug in foo.py" {
		t.Errorf("got %q", g)
	}
}

func TestLoadGoalEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, GoalFile), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGoal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if g != "" {
		t.Errorf("got %q, want empty", g)
	}
}
