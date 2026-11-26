package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) (dir string) {
	t.Helper()
	t.Setenv("ENVSET_BACKUP_DIR", filepath.Join(t.TempDir(), "backups"))
	return t.TempDir()
}

func TestWriteFileAtomicNewAndExisting(t *testing.T) {
	dir := setup(t)
	p := filepath.Join(dir, "f")
	if err := WriteFileAtomic(p, []byte("one"), 0o640); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("new file mode %v", fi.Mode().Perm())
	}
	os.Chmod(p, 0o600)
	if err := WriteFileAtomic(p, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("existing mode not kept: %v", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(p)
	if string(b) != "two" {
		t.Fatalf("content %q", b)
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestWriteFileAtomicDanglingSymlink(t *testing.T) {
	dir := setup(t)
	target := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(link, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("link replaced")
	}
	if b, _ := os.ReadFile(target); string(b) != "x" {
		t.Fatalf("target content %q", b)
	}
}

func TestBackupRestoreCycle(t *testing.T) {
	dir := setup(t)
	p := filepath.Join(dir, ".zshrc")

	if b, err := Backup(p); err != nil || b != "" {
		t.Fatalf("missing file should give no backup, got %q %v", b, err)
	}

	os.WriteFile(p, []byte("v1\n"), 0o644)
	b1, err := Backup(p)
	if err != nil || b1 == "" {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(b1); fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %v, want 0600", fi.Mode().Perm())
	}
	os.WriteFile(p, []byte("v2\n"), 0o644)

	restored, err := Restore(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if restored != b1 {
		t.Fatalf("restored %s, want %s", restored, b1)
	}
	if got, _ := os.ReadFile(p); string(got) != "v1\n" {
		t.Fatalf("after restore: %q", got)
	}

	// v2 was saved as a pre-restore backup and is not picked as "latest".
	list, _ := ListBackups(p)
	if len(list) != 2 || !list[0].PreRestore {
		t.Fatalf("unexpected backup list: %+v", list)
	}
	if got, _ := os.ReadFile(list[0].Path); string(got) != "v2\n" {
		t.Fatalf("pre-restore backup content %q", got)
	}
	// A second plain restore picks v1 again (not the pre-restore copy).
	if r, _ := Restore(p, ""); r != b1 {
		t.Fatalf("second restore picked %s", r)
	}
	// Undo via explicit backup path.
	if _, err := Restore(p, list[0].Path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "v2\n" {
		t.Fatalf("undo restore: %q", got)
	}
}

func TestBackupPruning(t *testing.T) {
	dir := setup(t)
	p := filepath.Join(dir, "f")
	os.WriteFile(p, []byte("x"), 0o644)
	for i := 0; i < MaxBackupsPerFile+5; i++ {
		if _, err := Backup(p); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := ListBackups(p)
	if len(list) != MaxBackupsPerFile {
		t.Fatalf("kept %d backups, want %d", len(list), MaxBackupsPerFile)
	}
}

func TestBackupsDoNotMixFiles(t *testing.T) {
	dir := setup(t)
	a := filepath.Join(dir, ".zsh")
	b := filepath.Join(dir, ".zshrc") // key of a is a prefix of key of b
	os.WriteFile(a, []byte("a"), 0o644)
	os.WriteFile(b, []byte("b"), 0o644)
	Backup(a)
	Backup(b)
	la, _ := ListBackups(a)
	if len(la) != 1 || !strings.Contains(la[0].Path, ".zsh.") {
		t.Fatalf("backups of %s: %+v", a, la)
	}
}

// chmod 400 means "do not change this": a rename must not bypass it.
func TestReadOnlyFileIsRespected(t *testing.T) {
	dir := setup(t)
	p := filepath.Join(dir, "rc")
	os.WriteFile(p, []byte("keep"), 0o400)
	link := filepath.Join(dir, "link")
	os.Symlink(p, link)
	for _, path := range []string{p, link} {
		if err := WriteFileAtomic(path, []byte("new"), 0o644); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("%s: err = %v", path, err)
		}
	}
	if got, _ := os.ReadFile(p); string(got) != "keep" {
		t.Fatalf("content %q", got)
	}
}

func TestLockAllWithAliasesDoesNotDeadlock(t *testing.T) {
	dir := setup(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	old := LockTimeout
	LockTimeout = 200 * time.Millisecond
	defer func() { LockTimeout = old }()
	p := filepath.Join(dir, "rc")
	os.WriteFile(p, []byte("x"), 0o644)
	link := filepath.Join(dir, "link")
	os.Symlink(p, link)
	unlock, err := LockAll([]string{p, link, p})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(link); err == nil {
		t.Fatal("link lock acquired while target is locked")
	}
	unlock()
	u, err := Lock(p)
	if err != nil {
		t.Fatal(err)
	}
	u()
}
