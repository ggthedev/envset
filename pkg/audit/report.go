package audit

import (
	"fmt"
	"io"
	"os"
	"sort"
)

// PrintReport writes a human-readable report to the given writer.
// Color is enabled when the writer is a TTY.
//
// Acknowledged findings (those matched by a custom user rule) are rendered in
// a separate "User-confirmed exceptions" section after the active findings,
// so impact stays visible without blocking the actionable list.
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

	// Partition active vs acknowledged.
	var active, acknowledged []Finding
	for _, f := range r.Findings {
		if f.Acknowledged {
			acknowledged = append(acknowledged, f)
		} else {
			active = append(active, f)
		}
	}

	// Active counts only — acknowledged exceptions don't drive the headline tally.
	activeCounts := map[Severity]int{SevError: 0, SevWarn: 0, SevInfo: 0}
	for _, f := range active {
		activeCounts[f.Severity]++
	}
	fmt.Fprintf(w, "Audit summary: %s, %s, %s",
		color(useColor, colorRed, fmt.Sprintf("%d error(s)", activeCounts[SevError])),
		color(useColor, colorYellow, fmt.Sprintf("%d warning(s)", activeCounts[SevWarn])),
		color(useColor, colorCyan, fmt.Sprintf("%d info", activeCounts[SevInfo])),
	)
	if len(acknowledged) > 0 {
		fmt.Fprintf(w, ", %s",
			color(useColor, colorBold, fmt.Sprintf("%d user-confirmed", len(acknowledged))),
		)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w)

	for i, f := range active {
		printFinding(w, useColor, i+1, f, false)
	}

	if len(acknowledged) > 0 {
		fmt.Fprintf(w, "%s\n\n",
			color(useColor, colorBold, "User-confirmed exceptions (custom rules):"),
		)
		for i, f := range acknowledged {
			printFinding(w, useColor, i+1, f, true)
		}
	}
}

func printFinding(w io.Writer, useColor bool, idx int, f Finding, acked bool) {
	label := severityLabel(f.Severity, useColor)
	tag := ""
	if acked {
		tag = " " + color(useColor, colorGreen, "[user-confirmed]")
	}
	fmt.Fprintf(w, "[%d] %s %s%s\n", idx, label, color(useColor, colorBold, f.Title), tag)
	if f.Line > 0 {
		fmt.Fprintf(w, "    %s:%d\n", f.File, f.Line)
	} else {
		fmt.Fprintf(w, "    %s\n", f.File)
	}
	if f.Code != "" {
		fmt.Fprintf(w, "    > %s\n", f.Code)
	}
	fmt.Fprintf(w, "    %s\n", f.Detail)
	if f.ApproxImpact != "" {
		fmt.Fprintf(w, "    %s %s\n",
			color(useColor, colorYellow, "approx impact:"), f.ApproxImpact)
	}
	if acked {
		if f.OverrideReason != "" {
			fmt.Fprintf(w, "    %s %s\n",
				color(useColor, colorGreen, "reason:"), f.OverrideReason)
		}
	} else {
		fmt.Fprintf(w, "    %s %s\n", color(useColor, colorGreen, "fix:"), f.Suggestion)
	}
	fmt.Fprintln(w)
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
