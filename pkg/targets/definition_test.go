package targets

import "testing"

func TestIsDefinitionLine(t *testing.T) {
	show := []string{
		`export A=1`, `# export A=1`, `  export A='x'`, `PATH="$PATH:/a"`, `A+=b`,
		`. "$HOME/.cargo/env"`, `source ~/.x`, `eval "$(rbenv init -)"`, `alias l='ls'`,
		`set -x A 1`, `[[ -d x ]] && export PATH="x:$PATH"`, `if true; then export A=1; fi`,
		`launchctl setenv A "1"`,
	}
	hide := []string{
		`if [[ -n $SSH_CONNECTION ]]; then`, `else`, `fi`, `# Preferred editor (available to all shell types)`,
		`}`, `echo hello`, `case $x in`,
	}
	for _, l := range show {
		if !IsDefinitionLine(l) {
			t.Errorf("hidden: %q", l)
		}
	}
	for _, l := range hide {
		if IsDefinitionLine(l) {
			t.Errorf("shown: %q", l)
		}
	}
}
