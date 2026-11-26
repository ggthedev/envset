package lazy

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestResolveKnownTool(t *testing.T) {
	spec, err := Resolve("pyenv", "zsh", "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "pyenv" {
		t.Errorf("expected name=pyenv, got %q", spec.Name)
	}
	if spec.InitCmd != "pyenv init - zsh" {
		t.Errorf("unexpected InitCmd: %q", spec.InitCmd)
	}
}

func TestResolveKnownToolBash(t *testing.T) {
	spec, err := Resolve("rbenv", "bash", "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.InitCmd != "rbenv init - bash" {
		t.Errorf("unexpected InitCmd: %q", spec.InitCmd)
	}
}

func TestResolveCustomInitCmd(t *testing.T) {
	spec, err := Resolve("mytool", "zsh", "mytool setup --fast")
	if err != nil {
		t.Fatal(err)
	}
	if spec.InitCmd != "mytool setup --fast" {
		t.Errorf("unexpected InitCmd: %q", spec.InitCmd)
	}
}

func TestResolveUnknownToolErrors(t *testing.T) {
	_, err := Resolve("unknowntool", "zsh", "")
	if err == nil {
		t.Fatal("expected error for unknown tool without --init-cmd")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGeneratePosix(t *testing.T) {
	spec := ToolSpec{Name: "pyenv", InitCmd: "pyenv init - zsh", Shell: "zsh"}
	out := Generate(spec)

	if !strings.Contains(out, "pyenv() {") {
		t.Error("output should contain function declaration")
	}
	if !strings.Contains(out, "unfunction pyenv") {
		t.Error("output should contain unfunction call")
	}
	if !strings.Contains(out, `eval "$(pyenv init - zsh)"`) {
		t.Error("output should contain eval init cmd")
	}
	if !strings.Contains(out, `pyenv "$@"`) {
		t.Error("output should pass through arguments")
	}
}

func TestGenerateFish(t *testing.T) {
	spec := ToolSpec{Name: "pyenv", InitCmd: "pyenv init - fish", Shell: "fish"}
	out := Generate(spec)

	if !strings.Contains(out, "function pyenv") {
		t.Error("output should contain fish function declaration")
	}
	if !strings.Contains(out, "functions --erase pyenv") {
		t.Error("output should erase the function")
	}
	if !strings.Contains(out, "pyenv init - fish | source") {
		t.Error("output should pipe init into source")
	}
	if !strings.Contains(out, "pyenv $argv") {
		t.Error("output should pass through arguments")
	}
}

func TestKnownToolNames(t *testing.T) {
	names := KnownToolNames()
	if len(names) == 0 {
		t.Fatal("expected at least some known tools")
	}
	found := false
	for _, n := range names {
		if n == "pyenv" {
			found = true
		}
	}
	if !found {
		t.Error("pyenv should be in known tools")
	}
}

func TestPromptAndHookToolsRefused(t *testing.T) {
	for _, tool := range []string{"starship", "oh-my-posh", "direnv", "atuin", "mcfly"} {
		for _, init := range []string{"", tool + " init zsh"} {
			if _, err := Resolve(tool, "zsh", init); err == nil || !strings.Contains(err.Error(), "can't be lazy-loaded") {
				t.Errorf("Resolve(%s, init=%q) err = %v", tool, init, err)
			}
		}
	}
}

func TestResolveRejectsScriptShells(t *testing.T) {
	if _, err := Resolve("pyenv", "sh", ""); err == nil {
		t.Fatal("sh accepted")
	}
}

// Run each generated wrapper in its real shell: init emits a multi-line script
// that redefines the tool, and the first call must reach that definition.
func TestWrappersRunInRealShells(t *testing.T) {
	inits := map[string]string{
		"zsh":  `printf '# c\nfaketool() {\n  echo "ready $1"\n}\n'`,
		"bash": `printf '# c\nfaketool() {\n  echo "ready $1"\n}\n'`,
		"fish": `printf '# c\nfunction faketool\n  echo "ready $argv"\nend\n'`,
	}
	flags := map[string][]string{"zsh": {"-f", "-c"}, "bash": {"--norc", "-c"}, "fish": {"--no-config", "-c"}}
	for shell, init := range inits {
		t.Run(shell, func(t *testing.T) {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s not installed", shell)
			}
			spec, err := Resolve("faketool", shell, init)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(path, append(flags[shell], Generate(spec)+"\nfaketool hi\n")...)
			cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "ready hi" {
				t.Fatalf("%s: %v: %q", shell, err, out)
			}
		})
	}
}

// The tool name becomes a function name written into a startup file.
func TestToolNameIsValidated(t *testing.T) {
	for _, bad := range []string{"x;touch y", "a b", "$(x)", "a\nb", "", "-x", "a/b", "f(){"} {
		if _, err := Resolve(bad, "zsh", "true"); err == nil {
			t.Errorf("tool name %q accepted", bad)
		}
	}
	for _, ok := range []string{"pyenv", "g++", "my_tool.v2", "oh-no"} {
		if _, err := Resolve(ok, "zsh", "true"); err != nil {
			t.Errorf("tool name %q rejected: %v", ok, err)
		}
	}
	if _, err := Resolve("tool", "zsh", "a\nrm x"); err == nil {
		t.Error("multi-line init command accepted")
	}
}
