package main

import (
	"os"
	"testing"
)

func Test_loadSystemPrompt_strips_frontmatter(t *testing.T) {
	content := "---\nmodel: gemini-3-flash\n---\n{{role \"system\"}}\n\nHello world\n"
	tmp := t.TempDir() + "/test.prompt"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := loadSystemPrompt(tmp)
	if err != nil {
		t.Fatal(err)
	}
	want := "Hello world"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func Test_run_fails_without_token(t *testing.T) {
	original := os.Getenv("TELEGRAM_BOT_TOKEN")
	os.Unsetenv("TELEGRAM_BOT_TOKEN")
	defer os.Setenv("TELEGRAM_BOT_TOKEN", original)

	err := run()
	if err == nil {
		t.Fatal("expected error when TELEGRAM_BOT_TOKEN is not set")
	}
}
