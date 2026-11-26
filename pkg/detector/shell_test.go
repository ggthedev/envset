package detector

import (
	"errors"
	"testing"
)

func TestParsePSLine(t *testing.T) {
	cases := []struct {
		in   string
		ppid int
		comm string
	}{
		{"  812 -zsh\n", 812, "-zsh"},
		{"1 /Applications/Visual Studio Code.app/Contents/MacOS/Electron", 1, "/Applications/Visual Studio Code.app/Contents/MacOS/Electron"},
		{"\t42\t/bin/bash", 42, "/bin/bash"},
	}
	for _, c := range cases {
		ppid, comm, err := parsePSLine(c.in)
		if err != nil || ppid != c.ppid || comm != c.comm {
			t.Errorf("parsePSLine(%q) = %d %q %v", c.in, ppid, comm, err)
		}
	}
	if _, _, err := parsePSLine("garbage"); err == nil {
		t.Error("expected error for garbage")
	}
}

func TestShellName(t *testing.T) {
	for in, want := range map[string]string{"-zsh": "zsh", "/bin/bash": "bash", "fish": "fish", "/usr/local/bin/-bash": "bash"} {
		if got := shellName(in); got != want {
			t.Errorf("shellName(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeTree builds a process walker from pid -> (ppid, comm).
func fakeTree(m map[int][2]any) func(int) (int, string, error) {
	return func(pid int) (int, string, error) {
		e, ok := m[pid]
		if !ok {
			return 0, "", errors.New("no such pid")
		}
		return e[0].(int), e[1].(string), nil
	}
}

func TestShellsInTree(t *testing.T) {
	// envset <- sh (script) <- Electron (path with spaces) <- -zsh <- login
	walk := fakeTree(map[int][2]any{
		100: {90, "/bin/sh"},
		90:  {80, "/Applications/Visual Studio Code.app/Contents/MacOS/Electron"},
		80:  {70, "-zsh"},
		70:  {1, "login"},
	})
	got := shellsInTree(100, walk)
	if len(got) != 2 || got[0] != "sh" || got[1] != "zsh" {
		t.Fatalf("shellsInTree = %v, want [sh zsh]", got)
	}
	// A cycle must not loop forever.
	loop := fakeTree(map[int][2]any{5: {6, "a"}, 6: {5, "b"}})
	if got := shellsInTree(5, loop); len(got) != 0 {
		t.Fatalf("cycle: %v", got)
	}
}

func TestResolveUserShell(t *testing.T) {
	none := func() string { return "" }
	dsclZsh := func() string { return "/bin/zsh" }
	cases := []struct {
		name               string
		flag, env, shellEv string
		tree               []string
		dscl               func() string
		want, src          string
	}{
		{"flag wins", "fish", "bash", "/bin/zsh", []string{"zsh"}, none, "fish", "--shell flag"},
		{"env override", "", "bash", "/bin/zsh", []string{"zsh"}, none, "bash", "$ENVSET_SHELL"},
		{"bash under zsh", "", "", "/bin/zsh", []string{"bash", "zsh"}, none, "bash", "process tree"},
		{"sh script under zsh", "", "", "/bin/zsh", []string{"sh", "zsh"}, none, "zsh", "process tree"},
		{"cron: sh only, SHELL=/bin/sh", "", "", "/bin/sh", []string{"sh"}, dsclZsh, "zsh", "directory service"},
		{"env -i: nothing", "", "", "", nil, none, "zsh", "default"},
		{"$SHELL bash, no tree", "", "", "/opt/homebrew/bin/bash", nil, none, "bash", "$SHELL"},
		{"bogus env override ignored", "", "csh", "/bin/bash", nil, none, "bash", "$SHELL"},
	}
	for _, c := range cases {
		got, src := resolveUserShell(c.flag, c.env, c.tree, c.shellEv, c.dscl)
		if got != c.want || src != c.src {
			t.Errorf("%s: got %s (%s), want %s (%s)", c.name, got, src, c.want, c.src)
		}
	}
}

func TestSetShellOverride(t *testing.T) {
	defer SetShellOverride("")
	if err := SetShellOverride("sh"); err == nil {
		t.Error("sh should be rejected")
	}
	if err := SetShellOverride("bash"); err != nil {
		t.Fatal(err)
	}
	info, err := Detect()
	if err != nil {
		t.Fatal(err)
	}
	if info.Shell != "bash" || info.ShellSource != "--shell flag" {
		t.Fatalf("got %s (%s)", info.Shell, info.ShellSource)
	}
}
