package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp creates a temp file with the given content and returns its path.
func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// readTemp reads a file and returns its content.
func readTemp(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBuildFixOps_BrokenPath(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, ".zshenv", `export FOO=bar
export PATH="echo node:$PATH"
export BAZ=qux
`)
	r := runAudit(t, map[string]string{
		".zshenv": `export FOO=bar
export PATH="echo node:$PATH"
export BAZ=qux
`,
	})

	ops, err := BuildFixOps(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) == 0 {
		t.Fatal("expected at least one FixOp for broken PATH")
	}
	found := false
	for _, op := range ops {
		if strings.Contains(op.OldLine, "echo node") {
			found = true
			if !strings.HasPrefix(strings.TrimSpace(op.NewLine), "# envset:") {
				t.Errorf("NewLine should start with '# envset:'; got %q", op.NewLine)
			}
		}
	}
	if !found {
		t.Error("no FixOp targeting the broken PATH line")
	}
}

func TestBuildFixOps_DuplicateCompinit(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshrc": `autoload -Uz compinit
compinit
# some other config
compinit -C
`,
	})

	ops, err := BuildFixOps(r)
	if err != nil {
		t.Fatal(err)
	}
	// Should fix the second compinit (line 4) but not the first (line 2).
	for _, op := range ops {
		if strings.Contains(op.OldLine, "compinit") {
			// The FIRST compinit should NOT be in the fix ops (we keep it).
			if op.Line == 2 {
				t.Errorf("line 2 (first compinit) should not be in fix ops")
			}
		}
	}
	found := false
	for _, op := range ops {
		if strings.Contains(op.OldLine, "compinit") && op.Line == 4 {
			found = true
		}
	}
	if !found {
		t.Error("expected FixOp for line 4 (duplicate compinit -C)")
	}
}

func TestBuildFixOps_DuplicateKeys(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshenv":            `export EDITOR=vim`,
		".gg_system_exports": `export EDITOR=nano`,
	})

	ops, err := BuildFixOps(r)
	if err != nil {
		t.Fatal(err)
	}
	// Should produce one fix op for the secondary duplicate.
	found := false
	for _, op := range ops {
		if strings.Contains(op.OldLine, "EDITOR") {
			found = true
			if !strings.HasPrefix(strings.TrimSpace(op.NewLine), "# envset:") {
				t.Errorf("NewLine should start with '# envset:'; got %q", op.NewLine)
			}
		}
	}
	if !found {
		t.Error("expected a FixOp for the duplicate EDITOR key")
	}
}

func TestBuildFixOps_AlreadyCommentedSkipped(t *testing.T) {
	// If the offending line is already commented out, BuildFixOps should skip it.
	r := runAudit(t, map[string]string{
		".zshenv": `# export PATH="echo bad:$PATH"
`,
	})
	// No broken PATH finding should be emitted (line is commented in .zshenv).
	ops, err := BuildFixOps(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if strings.Contains(op.OldLine, "echo bad") {
			t.Errorf("already-commented line should not appear in fix ops; got %+v", op)
		}
	}
}

func TestApplyFixes_WritesAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "zshenv", `export FOO=bar
export PATH="echo node:$PATH"
export BAZ=qux
`)

	ops := []FixOp{
		{
			File:    path,
			Line:    2,
			OldLine: `export PATH="echo node:$PATH"`,
			NewLine: `# envset: export PATH="echo node:$PATH"`,
		},
	}

	backups, err := ApplyFixes(ops)
	if err != nil {
		t.Fatal(err)
	}

	// Verify edited file.
	got := readTemp(t, path)
	if !strings.Contains(got, `# envset: export PATH="echo node:$PATH"`) {
		t.Errorf("edited file missing comment-out line; got:\n%s", got)
	}
	if strings.Contains(got, `export PATH="echo node:$PATH"`) && !strings.Contains(got, "# envset:") {
		t.Error("original broken line still present uncommented")
	}

	// Verify backup.
	if backups[path] == "" {
		t.Fatalf("expected a backup path for %s; got %v", path, backups)
	}
	bak := readTemp(t, backups[path])
	if !strings.Contains(bak, `export PATH="echo node:$PATH"`) {
		t.Errorf("backup should contain original content; got:\n%s", bak)
	}
}

func TestApplyFixes_Noop(t *testing.T) {
	// ApplyFixes with empty ops should return nil without touching anything.
	if _, err := ApplyFixes(nil); err != nil {
		t.Errorf("expected nil error for empty ops; got %v", err)
	}
}

func TestCountFixable(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshenv": `export PATH="echo node:$PATH"
`,
		".zshrc": `eval "$(direnv hook zsh)"
`,
	})
	n := CountFixable(r)
	// broken PATH is fixable; slow direnv init is NOT fixable.
	if n == 0 {
		t.Error("expected at least 1 fixable finding (broken PATH)")
	}
	// Verify the slow-init finding is NOT counted as fixable.
	for _, f := range r.Findings {
		if strings.Contains(f.Title, "Eager init") && f.Fixable() {
			t.Error("slow-init findings should NOT be auto-fixable")
		}
	}
}

func TestApplyFixes_RefusesStaleFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "zshenv", "export FOO=bar\nexport EDITED=yes\n")
	ops := []FixOp{{File: path, Line: 2, OldLine: `export PATH="echo node:$PATH"`, NewLine: "# envset: x"}}
	if _, err := ApplyFixes(ops); err == nil || !strings.Contains(err.Error(), "changed since the audit") {
		t.Fatalf("expected stale-file error, got %v", err)
	}
	if got := readTemp(t, path); got != "export FOO=bar\nexport EDITED=yes\n" {
		t.Fatalf("file must be untouched; got %q", got)
	}
}

func TestApplyFixes_PreservesOtherBytes(t *testing.T) {
	dir := t.TempDir()
	orig := "export A=1\n\n\nexport PATH=\"echo node:$PATH\"\n\n# tail\n"
	path := writeTemp(t, dir, "zshenv", orig)
	ops := []FixOp{{File: path, Line: 4, OldLine: `export PATH="echo node:$PATH"`, NewLine: `# envset: export PATH="echo node:$PATH"`}}
	if _, err := ApplyFixes(ops); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(orig, `export PATH="echo node:$PATH"`, `# envset: export PATH="echo node:$PATH"`, 1)
	if got := readTemp(t, path); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
