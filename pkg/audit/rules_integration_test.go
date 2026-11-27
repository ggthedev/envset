package audit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggthedev/envset/pkg/placer"
	"github.com/ggthedev/envset/pkg/rules"
)

// runAuditWithRules wires up a fake home and applies custom rules end-to-end.
func runAuditWithRules(t *testing.T, files map[string]string, custom rules.Custom) Report {
	t.Helper()
	home := t.TempDir()
	for name, content := range files {
		full := filepath.Join(home, name)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := placer.NewRegistry(home, "zsh")
	r := New(reg).Run()
	r.ApplyCustomRules(custom)
	return r
}

func TestApplyCustomRules_AcknowledgesSlowInitBySubject(t *testing.T) {
	home := t.TempDir()
	zshrc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(zshrc, []byte(`eval "$(pyenv init - zsh)"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := placer.NewRegistry(home, "zsh")
	r := New(reg).Run()

	if !contains(t, r, SevInfo, "Eager init") {
		t.Fatal("expected raw eager-init finding before override")
	}

	custom := rules.Custom{Rules: []rules.CustomRule{
		{
			CheckID: rules.CheckSlowInteractiveInit,
			Match:   rules.Match{File: zshrc, Subject: "pyenv"},
			Action:  rules.ActionAcknowledge,
			Reason:  "I want pyenv interactive-only on purpose.",
		},
	}}
	r.ApplyCustomRules(custom)

	// The finding should now be acknowledged (not removed from r.Findings).
	var got *Finding
	for i := range r.Findings {
		if r.Findings[i].CheckID == rules.CheckSlowInteractiveInit {
			got = &r.Findings[i]
			break
		}
	}
	if got == nil {
		t.Fatal("finding disappeared")
	}
	if !got.Acknowledged {
		t.Errorf("finding not acknowledged: %+v", got)
	}
	if got.OverrideReason == "" {
		t.Errorf("override reason missing: %+v", got)
	}
}

func TestApplyCustomRules_NoMatchLeavesFindingActive(t *testing.T) {
	r := runAuditWithRules(t,
		map[string]string{".zshrc": `eval "$(pyenv init - zsh)"` + "\n"},
		rules.Custom{Rules: []rules.CustomRule{
			{
				CheckID: rules.CheckSlowInteractiveInit,
				Match:   rules.Match{File: "/different/file", Subject: "pyenv"},
				Action:  rules.ActionAcknowledge,
			},
		}},
	)
	for _, f := range r.Findings {
		if f.CheckID == rules.CheckSlowInteractiveInit && f.Acknowledged {
			t.Errorf("rule for different file should not match; got acknowledged: %+v", f)
		}
	}
}

func TestApplyCustomRules_PathExportSubjectMatch(t *testing.T) {
	home := t.TempDir()
	zshrc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(zshrc,
		[]byte(`export PATH="$HOME/.rbenv/shims:$PATH"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := placer.NewRegistry(home, "zsh")
	r := New(reg).Run()

	// Find the PATH finding and confirm Subject was extracted.
	var pathFinding *Finding
	for i := range r.Findings {
		if r.Findings[i].CheckID == rules.CheckMisplacedPathExport {
			pathFinding = &r.Findings[i]
			break
		}
	}
	if pathFinding == nil {
		t.Fatal("expected misplaced-path finding")
	}
	if pathFinding.Subject != "$HOME/.rbenv/shims" {
		t.Errorf("subject extraction wrong: %q", pathFinding.Subject)
	}

	// Override by subject — should acknowledge.
	custom := rules.Custom{Rules: []rules.CustomRule{
		{
			CheckID: rules.CheckMisplacedPathExport,
			Match:   rules.Match{File: zshrc, Subject: "$HOME/.rbenv/shims"},
			Action:  rules.ActionAcknowledge,
		},
	}}
	r.ApplyCustomRules(custom)
	if !pathFinding.Acknowledged {
		t.Errorf("path-export finding not acknowledged after override: %+v", pathFinding)
	}
}
