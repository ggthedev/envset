package wizard

import (
	"testing"

	"github.com/ggthedev/envset/pkg/targets"
)

// Everything given on the command line is used as is: no question about it,
// and with --target no placement question either. A prompt would fail here
// because the test has no terminal.
func TestAddWizardHonoursFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := RunAddWizard(Preset{Key: "EDITOR", Value: "vim", Target: "zprofile", Type: "export"})
	if err != nil {
		t.Fatal(err)
	}
	want := AddAnswers{Key: "EDITOR", Value: "vim", Target: "zprofile", VarType: targets.VarTypeExport, Confirmed: true}
	if got != want {
		t.Fatalf("got %+v", got)
	}
}

func TestAddWizardPathTypeNeedsNoKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := RunAddWizard(Preset{Value: "/opt/x", Target: "zshenv", Type: "path_append"})
	if err != nil || got.Key != "PATH" || !got.Confirmed {
		t.Fatalf("%+v %v", got, err)
	}
}
