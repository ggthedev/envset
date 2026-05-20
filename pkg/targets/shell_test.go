package targets

import (
	"os"
	"strings"
	"testing"
)

func TestShellTargetAddListRemove(t *testing.T) {
	// Create a temp file to act as a fake .bashrc
	f, err := os.CreateTemp("", "envset_shell_test_*.bashrc")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	st := &ShellTarget{
		name:   "bashrc",
		path:   f.Name(),
		shell:  "bash",
		exists: true,
	}

	// Add export
	if err := st.Add("FOO", "bar", VarTypeExport, false); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// List
	entries, err := st.List("FOO")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Key != "FOO" || entries[0].Value != "bar" {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}

	// Remove (delete)
	if err := st.Remove("FOO", false, false); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	entries, err = st.List("FOO")
	if err != nil {
		t.Fatalf("list after remove failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after remove, got %d", len(entries))
	}
}

func TestShellTargetComment(t *testing.T) {
	f, err := os.CreateTemp("", "envset_shell_test_*.zshrc")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	st := &ShellTarget{
		name:   "zshrc",
		path:   f.Name(),
		shell:  "zsh",
		exists: true,
	}

	if err := st.Add("SECRET", "key123", VarTypeExport, false); err != nil {
		t.Fatal(err)
	}

	// Comment out
	if err := st.Remove("SECRET", true, false); err != nil {
		t.Fatalf("comment out failed: %v", err)
	}

	entries, err := st.List("SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].IsComment {
		t.Fatalf("expected commented entry, got: %s", entries[0].RawLine)
	}
}

func TestShellTargetPathAppend(t *testing.T) {
	f, err := os.CreateTemp("", "envset_shell_test_*.bashrc")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	st := &ShellTarget{
		name:   "bashrc",
		path:   f.Name(),
		shell:  "bash",
		exists: true,
	}

	if err := st.Add("", "/usr/local/bin", VarTypePathAppend, false); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(f.Name())
	if !strings.Contains(string(data), `export PATH="$PATH:/usr/local/bin"`) {
		t.Fatalf("unexpected file content: %s", string(data))
	}
}

func TestShellTargetPreview(t *testing.T) {
	st := &ShellTarget{shell: "bash"}

	p, err := st.Preview("FOO", "bar", VarTypeExport)
	if err != nil {
		t.Fatal(err)
	}
	if p != `export FOO=bar` {
		t.Fatalf("unexpected preview: %s", p)
	}

	p, err = st.Preview("", "$(rbenv init -)", VarTypeEval)
	if err != nil {
		t.Fatal(err)
	}
	if p != `eval $(rbenv init -)` {
		t.Fatalf("unexpected eval preview: %s", p)
	}
}
