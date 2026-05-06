package audit

import (
	"fmt"
	"io"
	"os"
	"sort"
)

// PrintReport writes a human-readable report to the given writer.
// Color is enabled when the writer is a TTY.
func PrintReport(w io.Writer, r Report) {
	useColor := isTerminal(w)

	if len(r.Findings) == 0 {
		fmt.Fprintln(w, color(useColor, colorGreen, "✓ No issues found. Your shell configs look clean."))
		return
	}

	// Sort: by severity (error first), then file, then line
	sort.SliceStable(r.Findings, func(i, j int) bool {
		si, sj := severityRank(r.Findings[i].Severity), severityRank(r.Findings[j].Severity)
		if si != sj {
			return si < sj
		}
		if r.Findings[i].File != r.Findings[j].File {
			return r.Findings[i].File < r.Findings[j].File
		}
		return r.Findings[i].Line < r.Findings[j].Line
	})

	counts := r.CountBySeverity()
	fmt.Fprintf(w, "Audit summary: %s, %s, %s\n\n",
		color(useColor, colorRed, fmt.Sprintf("%d error(s)", counts[SevError])),
		color(useColor, colorYellow, fmt.Sprintf("%d warning(s)", counts[SevWarn])),
		color(useColor, colorCyan, fmt.Sprintf("%d info", counts[SevInfo])),
	)

	for i, f := range r.Findings {
		label := severityLabel(f.Severity, useColor)
		fmt.Fprintf(w, "[%d] %s %s\n", i+1, label, color(useColor, colorBold, f.Title))
		if f.Line > 0 {
			fmt.Fprintf(w, "    %s:%d\n", f.File, f.Line)
		} else {
			fmt.Fprintf(w, "    %s\n", f.File)
		}
		if f.Code != "" {
			fmt.Fprintf(w, "    > %s\n", f.Code)
		}
		fmt.Fprintf(w, "    %s\n", f.Detail)
		fmt.Fprintf(w, "    %s %s\n", color(useColor, colorGreen, "fix:"), f.Suggestion)
		fmt.Fprintln(w)
	}
}

// ---------- color helpers ----------

const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
)

func color(enabled bool, c, s string) string {
	if !enabled {
		return s
	}
	return c + s + colorReset
}

func severityLabel(sv Severity, useColor bool) string {
	switch sv {
	case SevError:
		return color(useColor, colorRed, "ERROR  ")
	case SevWarn:
		return color(useColor, colorYellow, "WARNING")
	default:
		return color(useColor, colorCyan, "INFO   ")
	}
}

func severityRank(sv Severity) int {
	switch sv {
	case SevError:
		return 0
	case SevWarn:
		return 1
	default:
		return 2
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
