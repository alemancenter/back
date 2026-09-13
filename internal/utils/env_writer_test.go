package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateEnvFile_UpdatesExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("MAIL_HOST=old.example.com\nOTHER=keep\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := UpdateEnvFile(path, map[string]string{"MAIL_HOST": "new.example.com"}); err != nil {
		t.Fatalf("UpdateEnvFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "MAIL_HOST=new.example.com\nOTHER=keep\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A value containing a raw newline must never reach disk: unquoted (no space/tab in the
// injected value), it becomes its own real "KEY=VALUE" line once written, which a .env parser
// on the next restart reads as an independent assignment — letting an attacker who can set any
// single allowed setting value plant an arbitrary env var (see handlers/settings/handler.go's
// validateNoControlChars for the full attack: overwriting JWT_SECRET this way forges tokens for
// any account). UpdateEnvFile must reject the whole update rather than write it.
func TestUpdateEnvFile_RejectsEmbeddedNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	original := "MAIL_HOST=old.example.com\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	injected := "x\nJWT_SECRET=00000000000000000000000000000000"
	err := UpdateEnvFile(path, map[string]string{"MAIL_HOST": injected})
	if err == nil {
		t.Fatal("expected an error for a value containing a newline, got nil")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}
	if string(got) != original {
		t.Fatalf("file was modified despite the rejected update: got %q, want unchanged %q", got, original)
	}
}

func TestUpdateEnvFile_RejectsCarriageReturnAndNUL(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("MAIL_HOST=old.example.com\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for _, bad := range []string{"a\rb", "a\x00b"} {
		if err := UpdateEnvFile(path, map[string]string{"MAIL_HOST": bad}); err == nil {
			t.Fatalf("expected an error for value %q, got nil", bad)
		}
	}
}
