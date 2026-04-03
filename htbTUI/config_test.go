package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigPrefersLocalEnvOverReplay(t *testing.T) {
	previous, hadPrevious := os.LookupEnv("HTB_APP_TOKEN")
	if hadPrevious {
		defer os.Setenv("HTB_APP_TOKEN", previous)
	} else {
		defer os.Unsetenv("HTB_APP_TOKEN")
	}
	os.Unsetenv("HTB_APP_TOKEN")

	baseDir := t.TempDir()
	replayDir := filepath.Join(baseDir, "..", "HTB", "config")
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatalf("mkdir replay dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(replayDir, "replay.env"), []byte("HTB_APP_TOKEN=shared-token\n"), 0o600); err != nil {
		t.Fatalf("write replay env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, ".env"), []byte("HTB_APP_TOKEN=local-token\n"), 0o600); err != nil {
		t.Fatalf("write local env: %v", err)
	}

	config := loadConfig(baseDir)
	if got, want := config.Token, "local-token"; got != want {
		t.Fatalf("unexpected token: got %q want %q", got, want)
	}
}

func TestSaveTokenWritesOrUpdatesDotEnv(t *testing.T) {
	baseDir := t.TempDir()
	envFile := filepath.Join(baseDir, ".env")
	if err := os.WriteFile(envFile, []byte("HTB_API_BASE=https://example.test\n"), 0o600); err != nil {
		t.Fatalf("seed env file: %v", err)
	}

	config, err := saveToken(baseDir, `abc"123`)
	if err != nil {
		t.Fatalf("save token: %v", err)
	}

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, `HTB_APP_TOKEN="abc\"123"`) {
		t.Fatalf("saved token line missing or malformed: %q", text)
	}
	if got, want := config.Token, `abc"123`; got != want {
		t.Fatalf("unexpected token after reload: got %q want %q", got, want)
	}
}
