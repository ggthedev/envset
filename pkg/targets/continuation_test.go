package targets

import (
	"errors"
	"testing"
)

// After a trailing backslash the next line continues the same command, so a
// word such as "fi" or "if" there is an argument, not a keyword.
func TestKeywordsAfterBackslashAreArguments(t *testing.T) {
	// The block is still open: K must be refused, not edited.
	inside := "if a; then\n  echo \\\n    fi\n  export K=1\nfi\n"
	for _, body := range []string{inside, "if a; then\n  x && \\\n    echo \\\n    fi\n  export K=1\nfi\n"} {
		st, p := shellTarget(t, "zshrc", body)
		var refused *RefusedError
		if _, err := st.Add("K", "2", VarTypeExport, WriteOptions{}); !errors.As(err, &refused) {
			t.Errorf("%q: add err = %v, want refusal", body, err)
		}
		if _, err := st.Remove("K", RemoveOptions{}); !errors.As(err, &refused) {
			t.Errorf("%q: remove err = %v, want refusal", body, err)
		}
		if content(t, p) != body {
			t.Errorf("%q changed", body)
		}
	}
	// No block was opened: K stays editable.
	for _, pre := range []string{"echo \\\n  if\n", "echo a \\\n  case \\\n  do\n"} {
		st, p := shellTarget(t, "zshrc", pre+"export K=1\n")
		if _, err := st.Add("K", "2", VarTypeExport, WriteOptions{}); err != nil {
			t.Errorf("after %q: %v", pre, err)
		} else if got := content(t, p); got != pre+"export K=2\n" {
			t.Errorf("after %q: %q", pre, got)
		}
	}
	// "&& \" followed by a real keyword still opens a block.
	body := "a && \\\n  if b; then\n    export K=1\n  fi\n"
	st, _ := shellTarget(t, "zshrc", body)
	var refused *RefusedError
	if _, err := st.Add("K", "2", VarTypeExport, WriteOptions{}); !errors.As(err, &refused) {
		t.Errorf("keyword after '&& \\': %v", err)
	}
}
