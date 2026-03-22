package main

import (
	"os"
	"testing"
)

// should_return_error_when_bot_token_not_set verifies the fail-fast acceptance
// criterion: if TELEGRAM_BOT_TOKEN is not set, checkBotToken returns an error
// containing the expected message.
func Test_checkBotToken_should_return_error_when_bot_token_not_set(t *testing.T) {
	// Arrange
	original := os.Getenv("TELEGRAM_BOT_TOKEN")
	os.Unsetenv("TELEGRAM_BOT_TOKEN")
	defer os.Setenv("TELEGRAM_BOT_TOKEN", original)

	// Act
	err := checkBotToken()

	// Assert
	if err == nil {
		t.Fatal("expected error when TELEGRAM_BOT_TOKEN is not set, got nil")
	}
	want := "TELEGRAM_BOT_TOKEN is not set"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

// should_return_nil_when_bot_token_is_set verifies checkBotToken passes when
// the environment variable is present.
func Test_checkBotToken_should_return_nil_when_bot_token_is_set(t *testing.T) {
	// Arrange
	original := os.Getenv("TELEGRAM_BOT_TOKEN")
	os.Setenv("TELEGRAM_BOT_TOKEN", "fake-token-for-test")
	defer func() {
		if original == "" {
			os.Unsetenv("TELEGRAM_BOT_TOKEN")
		} else {
			os.Setenv("TELEGRAM_BOT_TOKEN", original)
		}
	}()

	// Act
	err := checkBotToken()

	// Assert
	if err != nil {
		t.Errorf("expected nil error when token is set, got: %v", err)
	}
}
