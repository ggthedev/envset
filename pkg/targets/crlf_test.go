package targets

import (
	"strings"
	"testing"
)

// Appending to a CRLF file keeps CRLF on every line.
func TestAppendKeepsCRLF(t *testing.T) {
	for _, body := range []string{"export A=1\r\nexport B=2\r\n", "export A=1\r\nexport B=2"} {
		st, p := shellTarget(t, "zshrc", body)
		if _, err := st.Add("K", "v", VarTypeExportAndPathAppend, WriteOptions{}); err != nil {
			t.Fatal(err)
		}
		got := content(t, p)
		if strings.Count(got, "\r\n") != strings.Count(got, "\n") || !strings.HasSuffix(got, "$K\"\r\n") {
			t.Errorf("mixed endings: %q", got)
		}
	}
}
