package placer

import (
	"strings"
	"testing"

	"github.com/ggthedev/envset/pkg/detector"
	"github.com/ggthedev/envset/pkg/targets"
)

func placerFor(home, shell string) *Placer {
	return NewPlacer(&detector.Info{OS: "darwin", Shell: shell, HomeDir: home})
}

// The user's real layout: JAVA_HOME lives in gg_system_exports, which
// .zprofile sources, so a new line in .zshenv would be overridden.
func TestSuggestsTheDefinitionShellsUse(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, ".gg_system_exports", "export JAVA_HOME=/old\n")
	writeFile(t, home, ".zprofile", `source "$HOME/.gg_system_exports"`+"\n")

	pls := placerFor(home, "zsh").Analyze(DetectIntent("JAVA_HOME", "/new", targets.VarTypeExport))
	if pls[0].File.Name != "gg_system_exports" || !pls[0].Update {
		t.Fatalf("top = %s (%s)", pls[0].File.Name, pls[0].Reason)
	}

	reg := placerFor(home, "zsh").Registry
	zshenv, _ := reg.FindByName("zshenv")
	notes := reg.DuplicateNotes(*zshenv, "JAVA_HOME")
	if len(notes) != 1 || !strings.Contains(notes[0], "login shells will use that value") ||
		!strings.Contains(notes[0], "--target gg_system_exports") {
		t.Fatalf("zshenv notes: %q", notes)
	}
	// Written to .zshrc, which loads last, the new value wins.
	zshrc, _ := reg.FindByName("zshrc")
	if notes := reg.DuplicateNotes(*zshrc, "JAVA_HOME"); len(notes) != 1 || !strings.Contains(notes[0], "takes precedence") {
		t.Fatalf("zshrc notes: %q", notes)
	}
}

func TestSourcePositionDecidesWithinOneFile(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, ".gg_system_exports", "export K=gg\n")
	writeFile(t, home, ".zshenv", "source ~/.gg_system_exports\n")
	reg := NewRegistry(home, "zsh")
	zshenv, _ := reg.FindByName("zshenv")
	// Appended after the source line: the new value wins.
	if n := reg.DuplicateNotes(*zshenv, "K"); len(n) != 1 || !strings.Contains(n[0], "takes precedence") {
		t.Fatalf("append after source: %q", n)
	}
	// An existing line above the source line is updated in place: gg still wins.
	writeFile(t, home, ".zshenv", "export K=env\nsource ~/.gg_system_exports\n")
	reg = NewRegistry(home, "zsh")
	zshenv, _ = reg.FindByName("zshenv")
	if n := reg.DuplicateNotes(*zshenv, "K"); len(n) != 1 || !strings.Contains(n[0], "will use that value") {
		t.Fatalf("update above source: %q", n)
	}
}

func TestIdenticalAndCommentedAndPATH(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, ".zshrc", "# export FOO=new\nexport BAR=1\nexport PATH=\"$PATH:/a\"\n")
	p := placerFor(home, "zsh")

	for _, pl := range p.Analyze(DetectIntent("FOO", "new", targets.VarTypeExport)) {
		if pl.Update {
			t.Fatalf("commented-out line counted as existing: %s", pl.File.Name)
		}
	}
	top := p.Analyze(DetectIntent("BAR", "1", targets.VarTypeExport))[0]
	if !top.Identical {
		t.Fatalf("identical value not detected: %+v", top)
	}
	if top := p.Analyze(DetectIntent("BAR", "2", targets.VarTypeExport))[0]; top.Identical || !top.Update {
		t.Fatalf("changed value: %+v", top)
	}
	top = p.Analyze(DetectIntent("PATH", "/b", targets.VarTypePathAppend))[0]
	if top.File.Name != "zshenv" || top.Update {
		t.Fatalf("PATH went to %s (update=%v)", top.File.Name, top.Update)
	}
}

// Every shell must get a real file, with or without gg_system files.
func TestEveryShellGetsATarget(t *testing.T) {
	cases := []struct{ shell, files, want string }{
		{"bash", "", "bash_profile"},
		{"bash", ".profile", "profile"}, // don't create .bash_profile over .profile
		{"fish", "", "config_fish"},
		{"zsh", "", "zshenv"},
	}
	for _, c := range cases {
		home := t.TempDir()
		if c.files != "" {
			writeFile(t, home, c.files, "x=1\n")
		}
		for _, in := range []Intent{
			DetectIntent("PATH", "/x", targets.VarTypePathAppend),
			DetectIntent("JAVA_HOME", "/j", targets.VarTypeExport),
		} {
			if got := placerFor(home, c.shell).Analyze(in)[0].File.Name; got != c.want {
				t.Errorf("%s %q %s: placed in %q, want %q", c.shell, c.files, in.Key, got, c.want)
			}
		}
	}
}

func TestChosenScopeIsHonoured(t *testing.T) {
	home := t.TempDir()
	for scope, want := range map[Scope]string{ScopeLogin: "zprofile", ScopeAllSessions: "zshenv", ScopeInteractive: "zshrc"} {
		in := DetectIntent("SOMETHING", "1", targets.VarTypeExport).withChosenScope(scope)
		if in.Scope != scope {
			t.Fatalf("scope overwritten: %s", in.Scope)
		}
		if got := placerFor(home, "zsh").Analyze(in)[0].File.Name; got != want {
			t.Errorf("chose %s: placed in %s, want %s", scope, got, want)
		}
	}
}

func TestExportAndPathAppendIsAllSessions(t *testing.T) {
	in := DetectIntent("TOOL_HOME", "/t", targets.VarTypeExportAndPathAppend)
	if in.Scope != ScopeAllSessions || in.Category != CatSystem {
		t.Fatalf("%+v", in)
	}
}

func TestStartupFiles(t *testing.T) {
	home := t.TempDir()
	for _, f := range []string{".zshrc", ".zshenv", ".zprofile", ".gg_system_exports", ".bashrc", ".bash_profile", ".profile"} {
		writeFile(t, home, f, "x=1\n")
	}
	names := func(shell string) string {
		var out []string
		for _, p := range NewRegistry(home, shell).StartupFiles() {
			out = append(out, strings.TrimPrefix(p, home+"/"))
		}
		return strings.Join(out, " ")
	}
	if got := names("zsh"); got != ".zshenv .zprofile .zshrc" {
		t.Errorf("zsh: %s", got)
	}
	if got := names("bash"); got != ".bash_profile .bashrc" {
		t.Errorf("bash: %s", got)
	}
	writeFile(t, home, ".bash_profile", "source ~/.bashrc\n")
	if got := names("bash"); got != ".bash_profile" {
		t.Errorf("bash with sourced bashrc: %s", got)
	}
}
