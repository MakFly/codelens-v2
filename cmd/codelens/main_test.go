package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectPathFlagWins(t *testing.T) {
	cwd := t.TempDir()
	flagProject := filepath.Join(cwd, "flag-project")
	if err := os.MkdirAll(flagProject, 0o755); err != nil {
		t.Fatalf("mkdir flag project: %v", err)
	}

	got, err := resolveProjectPath(flagProject, true, "/env/project", true, cwd)
	if err != nil {
		t.Fatalf("resolveProjectPath: %v", err)
	}
	if got.Source != "flag" {
		t.Fatalf("expected source=flag, got %q", got.Source)
	}
	if got.Path != flagProject {
		t.Fatalf("expected path=%q, got %q", flagProject, got.Path)
	}
}

func TestResolveProjectPathEnvWinsWhenNoFlag(t *testing.T) {
	cwd := t.TempDir()
	envProject := filepath.Join(cwd, "env-project")
	if err := os.MkdirAll(envProject, 0o755); err != nil {
		t.Fatalf("mkdir env project: %v", err)
	}

	got, err := resolveProjectPath(".", false, envProject, true, cwd)
	if err != nil {
		t.Fatalf("resolveProjectPath: %v", err)
	}
	if got.Source != "env" {
		t.Fatalf("expected source=env, got %q", got.Source)
	}
	if got.Path != envProject {
		t.Fatalf("expected path=%q, got %q", envProject, got.Path)
	}
}

func TestResolveProjectPathAutoNearestCodelensFromCwd(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "repo")
	nested := filepath.Join(project, "apps", "api", "src")

	if err := os.MkdirAll(filepath.Join(project, ".codelens"), 0o755); err != nil {
		t.Fatalf("mkdir .codelens: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, ".codelens", "index.db"), []byte(""), 0o644); err != nil {
		t.Fatalf("write index.db: %v", err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	got, err := resolveProjectPath(".", false, "", false, nested)
	if err != nil {
		t.Fatalf("resolveProjectPath: %v", err)
	}
	if got.Source != "auto" {
		t.Fatalf("expected source=auto, got %q", got.Source)
	}
	if got.Path != project {
		t.Fatalf("expected path=%q, got %q", project, got.Path)
	}
}

func TestResolveProjectPathFallbackToCwdWhenNoCodelens(t *testing.T) {
	cwd := t.TempDir()
	if ancestor, ok := findIndexedAncestor(cwd); ok {
		t.Skipf("environment has indexed ancestor %q; fallback scenario not deterministic", ancestor)
	}

	got, err := resolveProjectPath(".", false, "", false, cwd)
	if err != nil {
		t.Fatalf("resolveProjectPath: %v", err)
	}
	if got.Source != "fallback" {
		t.Fatalf("expected source=fallback, got %q", got.Source)
	}
	if got.Path != cwd {
		t.Fatalf("expected path=%q, got %q", cwd, got.Path)
	}
}

func findIndexedAncestor(start string) (string, bool) {
	absStart, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	current := filepath.Clean(absStart)
	for {
		dbPath := filepath.Join(current, ".codelens", "index.db")
		if fi, statErr := os.Stat(dbPath); statErr == nil && !fi.IsDir() {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func TestResolveDBPath_DefaultRelativeAbsolute(t *testing.T) {
	project := "/tmp/my-project"

	gotDefault, err := resolveDBPath(project, "")
	if err != nil {
		t.Fatalf("resolveDBPath default: %v", err)
	}
	wantDefault := filepath.Join(project, ".codelens", "index.db")
	if gotDefault != wantDefault {
		t.Fatalf("expected default db=%q, got %q", wantDefault, gotDefault)
	}

	gotRelative, err := resolveDBPath(project, "custom/index.db")
	if err != nil {
		t.Fatalf("resolveDBPath relative: %v", err)
	}
	wantRelative := filepath.Join(project, "custom", "index.db")
	if gotRelative != wantRelative {
		t.Fatalf("expected relative db=%q, got %q", wantRelative, gotRelative)
	}

	abs := "/var/tmp/index.db"
	gotAbs, err := resolveDBPath(project, abs)
	if err != nil {
		t.Fatalf("resolveDBPath absolute: %v", err)
	}
	if gotAbs != abs {
		t.Fatalf("expected absolute db=%q, got %q", abs, gotAbs)
	}
}
