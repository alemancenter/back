package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoragePathBoundary(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.pdf"), []byte("pdf"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../outside", "", "/etc/passwd", "a\\b", "escape/secret.pdf"} {
		if _, err := ResolveStoragePath(root, p); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
	for _, p := range []string{"ok.pdf", "new/missing.pdf"} {
		if _, err := ResolveStoragePath(root, p); err != nil {
			t.Errorf("rejected %q: %v", p, err)
		}
	}
}
