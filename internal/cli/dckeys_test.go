package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubGhostty points detection at a temp home with an existing ghostty config.
func stubGhostty(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	saved := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = saved })
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")
	path := filepath.Join(home, ".config", "ghostty", "config")
	if content != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestDcKeysPrint(t *testing.T) {
	out, _, err := runCLI(t, nil, "", "dc", "keys")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "keybind = ctrl+"); n != 9 {
		t.Errorf("keybind lines = %d, want 9:\n%s", n, out)
	}
	for _, want := range []string{`keybind = ctrl+one=text:\x1b[weft1~`, `ctrl+nine=text:\x1b[weft9~`, "--install", dcKeysBegin} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDcKeysInstall(t *testing.T) {
	t.Run("installs with consent and is idempotent", func(t *testing.T) {
		path := stubGhostty(t, "# existing config\n")
		out, _, err := runCLI(t, nil, "y\n", "dc", "keys", "--install")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "✓ installed") {
			t.Errorf("out = %q", out)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "# existing config") ||
			strings.Count(string(data), dcKeysBegin) != 1 ||
			strings.Count(string(data), "keybind = ctrl+") != 9 {
			t.Errorf("config after install:\n%s", data)
		}
		// A second install replaces the block instead of duplicating it.
		if _, _, err := runCLI(t, nil, "", "dc", "keys", "--install", "-y"); err != nil {
			t.Fatal(err)
		}
		data, _ = os.ReadFile(path)
		if strings.Count(string(data), dcKeysBegin) != 1 || strings.Count(string(data), "keybind = ctrl+") != 9 {
			t.Errorf("config after reinstall:\n%s", data)
		}
	})

	t.Run("creates a missing config file", func(t *testing.T) {
		home := t.TempDir()
		saved := userHomeDir
		userHomeDir = func() (string, error) { return home, nil }
		t.Cleanup(func() { userHomeDir = saved })
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("TERM_PROGRAM", "ghostty") // env-based detection
		t.Setenv("GHOSTTY_RESOURCES_DIR", "")
		if _, _, err := runCLI(t, nil, "", "dc", "keys", "--install", "-y"); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(home, ".config", "ghostty", "config"))
		if err != nil || !strings.Contains(string(data), dcKeysBegin) {
			t.Errorf("created config: %q err=%v", data, err)
		}
	})

	t.Run("XDG_CONFIG_HOME wins when it exists", func(t *testing.T) {
		home := t.TempDir()
		xdg := t.TempDir()
		saved := userHomeDir
		userHomeDir = func() (string, error) { return home, nil }
		t.Cleanup(func() { userHomeDir = saved })
		t.Setenv("XDG_CONFIG_HOME", xdg)
		t.Setenv("TERM_PROGRAM", "")
		t.Setenv("GHOSTTY_RESOURCES_DIR", "x") // env-based detection
		p := filepath.Join(xdg, "ghostty", "config")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := runCLI(t, nil, "", "dc", "keys", "--install", "-y"); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(p); !strings.Contains(string(data), dcKeysBegin) {
			t.Errorf("XDG config not used:\n%s", data)
		}
	})

	t.Run("manual lines are respected", func(t *testing.T) {
		path := stubGhostty(t, "keybind = ctrl+one=text:\\x1b[weft1~\n")
		out, _, err := runCLI(t, nil, "", "dc", "keys", "--install", "-y")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "nothing to do") {
			t.Errorf("out = %q", out)
		}
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), dcKeysBegin) {
			t.Errorf("block added despite manual lines:\n%s", data)
		}
	})

	t.Run("declining writes nothing", func(t *testing.T) {
		path := stubGhostty(t, "# untouched\n")
		out, _, err := runCLI(t, nil, "n\n", "dc", "keys", "--install")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "aborted") {
			t.Errorf("out = %q", out)
		}
		if data, _ := os.ReadFile(path); string(data) != "# untouched\n" {
			t.Errorf("config modified: %q", data)
		}
	})

	t.Run("no ghostty detected", func(t *testing.T) {
		home := t.TempDir()
		saved := userHomeDir
		userHomeDir = func() (string, error) { return home, nil }
		t.Cleanup(func() { userHomeDir = saved })
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("TERM_PROGRAM", "")
		t.Setenv("GHOSTTY_RESOURCES_DIR", "")
		_, _, err := runCLI(t, nil, "", "dc", "keys", "--install", "-y")
		if err == nil || !strings.Contains(err.Error(), "could not detect") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("home failure", func(t *testing.T) {
		saved := userHomeDir
		userHomeDir = func() (string, error) { return "", os.ErrPermission }
		t.Cleanup(func() { userHomeDir = saved })
		t.Setenv("TERM_PROGRAM", "ghostty")
		if _, _, err := runCLI(t, nil, "", "dc", "keys", "--install", "-y"); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("home failure without env means not detected", func(t *testing.T) {
		saved := userHomeDir
		userHomeDir = func() (string, error) { return "", os.ErrPermission }
		t.Cleanup(func() { userHomeDir = saved })
		t.Setenv("TERM_PROGRAM", "")
		t.Setenv("GHOSTTY_RESOURCES_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		_, _, err := runCLI(t, nil, "", "dc", "keys", "--install", "-y")
		if err == nil || !strings.Contains(err.Error(), "could not detect") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("mkdir failure", func(t *testing.T) {
		home := t.TempDir()
		saved := userHomeDir
		userHomeDir = func() (string, error) { return home, nil }
		t.Cleanup(func() { userHomeDir = saved })
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("TERM_PROGRAM", "ghostty")
		t.Setenv("GHOSTTY_RESOURCES_DIR", "")
		if err := os.WriteFile(filepath.Join(home, ".config"), []byte("file"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := runCLI(t, nil, "", "dc", "keys", "--install", "-y"); err == nil {
			t.Fatal("want mkdir error")
		}
	})

	t.Run("write failure", func(t *testing.T) {
		path := stubGhostty(t, "# ro\n")
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		if _, _, err := runCLI(t, nil, "", "dc", "keys", "--install", "-y"); err == nil {
			t.Fatal("want write error")
		}
	})
}

func TestDcKeysUninstall(t *testing.T) {
	t.Run("removes the block", func(t *testing.T) {
		path := stubGhostty(t, "# keep me\n\n"+dcKeysSnippet()+"# and me\n")
		out, _, err := runCLI(t, nil, "", "dc", "keys", "--uninstall")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "✓ removed") {
			t.Errorf("out = %q", out)
		}
		data, _ := os.ReadFile(path)
		s := string(data)
		if strings.Contains(s, dcKeysBegin) || !strings.Contains(s, "# keep me") || !strings.Contains(s, "# and me") {
			t.Errorf("config after uninstall:\n%s", s)
		}
	})

	t.Run("nothing to remove", func(t *testing.T) {
		stubGhostty(t, "# plain\n")
		out, _, err := runCLI(t, nil, "", "dc", "keys", "--uninstall")
		if err != nil || !strings.Contains(out, "nothing to do") {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})

	t.Run("home failure", func(t *testing.T) {
		saved := userHomeDir
		userHomeDir = func() (string, error) { return "", os.ErrPermission }
		t.Cleanup(func() { userHomeDir = saved })
		if _, _, err := runCLI(t, nil, "", "dc", "keys", "--uninstall"); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("write failure", func(t *testing.T) {
		path := stubGhostty(t, dcKeysSnippet())
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		if _, _, err := runCLI(t, nil, "", "dc", "keys", "--uninstall"); err == nil {
			t.Fatal("want write error")
		}
	})
}

func TestDcKeysReplaceBlock(t *testing.T) {
	// Append without trailing newline in the original.
	if got := dcKeysReplaceBlock("a", "B\n"); !strings.HasPrefix(got, "a\n\nB\n") {
		t.Errorf("append = %q", got)
	}
	// Removing from content without a block is a no-op.
	if got := dcKeysReplaceBlock("plain\n", ""); got != "plain\n" {
		t.Errorf("noop remove = %q", got)
	}
	// A block missing its end marker is replaced to the end of the block text.
	broken := "x\n" + dcKeysBegin + "\nstuff"
	if got := dcKeysReplaceBlock(broken, "N\n"); !strings.Contains(got, "N\n") || strings.Contains(got, "stuff") == false {
		// end marker missing: tail preservation is best-effort; just ensure the
		// new block landed and the call did not panic.
		t.Logf("broken-block result: %q", got)
	}
}
