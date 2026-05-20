package lazy

import (
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
	spec := ToolSpec{Name: "direnv", InitCmd: "direnv hook fish", Shell: "fish"}
	out := Generate(spec)

	if !strings.Contains(out, "function direnv") {
		t.Error("output should contain fish function declaration")
	}
	if !strings.Contains(out, "functions --erase direnv") {
		t.Error("output should erase the function")
	}
	if !strings.Contains(out, "(direnv hook fish)") {
		t.Error("output should contain fish command substitution")
	}
	if !strings.Contains(out, "direnv $argv") {
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
