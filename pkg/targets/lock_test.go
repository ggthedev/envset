package targets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/experimentalist/envset/pkg/fsutil"
)

// Concurrent writers must not overwrite each other's changes, including when
// they reach the same file through a symlink.
func TestConcurrentAddsAllSurvive(t *testing.T) {
	home := t.TempDir()
	st, _ := NewShellTarget("zshrc", home)
	os.WriteFile(st.Path(), []byte("# base\n"), 0o644)
	link := filepath.Join(home, "alias.zsh")
	if err := os.Symlink(st.Path(), link); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "zshrc"
			if i%2 == 1 {
				name = link
			}
			tg, _ := Factory(name, home)
			if _, err := tg.Add(fmt.Sprintf("P%d", i), "v", VarTypeExport, WriteOptions{}); err != nil {
				t.Errorf("add %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	got := content(t, st.Path())
	for i := 0; i < 24; i++ {
		if !strings.Contains(got, fmt.Sprintf("export P%d=v\n", i)) {
			t.Errorf("P%d lost", i)
		}
	}
}

func TestWritersWaitForTheLock(t *testing.T) {
	st, p := shellTarget(t, "zshrc", "export K=1\n")
	unlock, err := fsutil.Lock(p)
	if err != nil {
		t.Fatal(err)
	}
	old := fsutil.LockTimeout
	fsutil.LockTimeout = 150 * time.Millisecond
	defer func() { fsutil.LockTimeout = old }()

	if _, err := st.Add("K", "2", VarTypeExport, WriteOptions{}); err == nil || !strings.Contains(err.Error(), "another envset") {
		t.Errorf("add while locked: %v", err)
	}
	if _, err := st.Remove("K", RemoveOptions{}); err == nil {
		t.Error("remove while locked succeeded")
	}
	if _, err := st.Add("K", "2", VarTypeExport, WriteOptions{DryRun: true}); err != nil {
		t.Errorf("dry run needs no lock: %v", err)
	}
	if content(t, p) != "export K=1\n" {
		t.Fatal("changed while locked")
	}
	unlock()
	if _, err := st.Add("K", "2", VarTypeExport, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
}

// An editor saving between envset's read and write must not be overwritten.
func TestCommitRefusesIfFileChanged(t *testing.T) {
	_, p := shellTarget(t, "zshrc", "export A=editor-saved\n")
	for _, c := range []struct {
		old     string
		existed bool
	}{{"export A=what-envset-read\n", true}, {"", false}} {
		_, err := CommitFile(p, "zsh", []byte(c.old), c.existed, []byte("export A=envset\n"), 0o644)
		if err == nil || !strings.Contains(err.Error(), "changed while") {
			t.Errorf("existed=%v: %v", c.existed, err)
		}
	}
	if content(t, p) != "export A=editor-saved\n" {
		t.Fatal("overwrote the newer content")
	}
}
