package downloader_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"yaria/internal/yaria/downloader"
)

func TestEnsureAria2Download(t *testing.T) {
	// Hide system aria2 so we exercise the download path
	t.Setenv("PATH", "/usr/bin:/bin")
	// If aria2c still on PATH via /usr/bin symlink, rename check uses LookPath
	if p, err := exec.LookPath("aria2c"); err == nil {
		// put empty dir first
		empty := t.TempDir()
		t.Setenv("PATH", empty)
		_ = p
	}
	dir := filepath.Join(t.TempDir(), "deps")
	os.MkdirAll(dir, 0755)
	// remove any cached
	os.Remove(filepath.Join(dir, "aria2c"))
	p, err := downloader.EnsureAria2(dir, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() < 100_000 {
		t.Fatalf("binary too small: %d (path %s)", fi.Size(), p)
	}
	// must be in deps dir when system was hidden
	if filepath.Dir(p) != dir {
		t.Fatalf("expected install into deps dir, got %s", p)
	}
	cmd := exec.Command(p, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v %s", err, out)
	}
	t.Logf("ok %s (%d bytes) %s", p, fi.Size(), string(out[:min(80, len(out))]))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
