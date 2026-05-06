package audit

import (
	"strings"
	"testing"
)

func splitLines(s string) []string {
	return strings.Split(strings.TrimPrefix(s, "\n"), "\n")
}

func TestBlockContext_InCase(t *testing.T) {
	src := `
case "$TERM_PROGRAM" in
    Tabby)
        eval "$(oh-my-posh init zsh)"
        ;;
    *)
        ;;
esac
eval "$(oh-my-posh init zsh)"
`
	// 1-based mapping after splitLines (leading "\n" trimmed):
	// 1: case "$TERM_PROGRAM" in
	// 2:     Tabby)
	// 3:         eval "$(oh-my-posh init zsh)"
	// 4:         ;;
	// 5:     *)
	// 6:         ;;
	// 7: esac
	// 8: eval "$(oh-my-posh init zsh)"
	lines := splitLines(src)
	bc := NewBlockContext(lines)

	if !bc.InCase(3) {
		t.Errorf("line 3 (eval inside Tabby branch) should be inside case")
	}
	if bc.InCase(8) {
		t.Errorf("line 8 (after esac) should NOT be inside case")
	}
	// `case ... in` line itself is not inside (snapshot before opener).
	if bc.InCase(1) {
		t.Errorf("line 1 (case header) should NOT be inside")
	}
	// `esac` line itself is not inside (snapshot after closer).
	if bc.InCase(7) {
		t.Errorf("line 7 (esac) should NOT be inside")
	}
}

func TestBlockContext_InCachePrimeGuard(t *testing.T) {
	src := `
if [[ ! -f "$HOME/.cache/fnm-init.zsh" ]]; then
    fnm env --use-on-cd > "$HOME/.cache/fnm-init.zsh"
fi
source "$HOME/.cache/fnm-init.zsh"
`
	// 1: if [[ ! -f ... ]]; then
	// 2:     fnm env ... > cache
	// 3: fi
	// 4: source cache
	lines := splitLines(src)
	bc := NewBlockContext(lines)

	if !bc.InCachePrimeGuard(2) {
		t.Errorf("line 2 (fnm env body) should be inside cache-prime guard; ifPreds=%v", bc.ifPreds[1])
	}
	if bc.InCachePrimeGuard(4) {
		t.Errorf("line 4 (after fi) should NOT be inside guard")
	}
}

func TestBlockContext_PlainIfNotCacheGuard(t *testing.T) {
	src := `
if command -v fnm &> /dev/null; then
    eval "$(fnm env)"
fi
`
	// 1: if command -v ...; then
	// 2:     eval "$(fnm env)"
	// 3: fi
	lines := splitLines(src)
	bc := NewBlockContext(lines)

	if bc.InCachePrimeGuard(2) {
		t.Errorf("plain command -v guard should not match cache-prime")
	}
}

func TestBlockContext_NestedIfPopsCorrectly(t *testing.T) {
	src := `
if command -v fnm &> /dev/null; then
    if [[ ! -f $cache ]]; then
        fnm env > $cache
    fi
    source $cache
fi
`
	// 1: if command -v fnm ...; then
	// 2:     if [[ ! -f $cache ]]; then
	// 3:         fnm env > $cache
	// 4:     fi
	// 5:     source $cache
	// 6: fi
	lines := splitLines(src)
	bc := NewBlockContext(lines)

	if !bc.InCachePrimeGuard(3) {
		t.Errorf("line 3 (inside both ifs) should be inside cache-prime")
	}
	if bc.InCachePrimeGuard(5) {
		t.Errorf("line 5 (after inner fi) should NOT be inside cache-prime")
	}
}

func TestSlowInit_CaseBranchSuppressed(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshrc": `case "$TERM_PROGRAM" in
    Tabby)
        eval "$(oh-my-posh init zsh)"
        ;;
esac
`,
	})
	if contains(t, r, SevInfo, "Eager init") {
		t.Fatalf("case-branch oh-my-posh should NOT be flagged; got %+v", r.Findings)
	}
}

func TestSlowInit_CachePrimeSuppressed(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshrc": `if command -v fnm &> /dev/null; then
    if [[ ! -f "$HOME/.cache/fnm-init.zsh" ]]; then
        fnm env --use-on-cd > "$HOME/.cache/fnm-init.zsh"
    fi
    source "$HOME/.cache/fnm-init.zsh"
fi
`,
	})
	if contains(t, r, SevInfo, "Eager init") {
		t.Fatalf("cache-prime fnm should NOT be flagged; got %+v", r.Findings)
	}
}

func TestSlowInit_BareEvalStillFlagged(t *testing.T) {
	r := runAudit(t, map[string]string{
		".zshrc": `eval "$(oh-my-posh init zsh)"
`,
	})
	if !contains(t, r, SevInfo, "Eager init") {
		t.Fatalf("bare eval should still be flagged; got %+v", r.Findings)
	}
}
