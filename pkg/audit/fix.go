package audit

import (
	"fmt"
	"github.com/experimentalist/envset/pkg/fsutil"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/experimentalist/envset/pkg/targets"
)

// FixOp is a single comment-out edit derived from a Finding.Fix.
type FixOp struct {
	File    string // absolute path of the target file
	Line    int    // 1-indexed line number to edit
	OldLine string // original line content (for diff display)
	NewLine string // replacement content (comment-out form)
}

// BuildFixOps derives a sorted, de-duplicated slice of FixOps from a Report.
// Only findings with a non-nil Fix field contribute operations.
// Lines that are already commented out are silently skipped.
func BuildFixOps(r Report) ([]FixOp, error) {
	// Load each file referenced by any fix edit once.
	fileCache := map[string][]string{}
	for _, f := range r.Findings {
		if f.Fix == nil {
			continue
		}
		for _, e := range f.Fix.Edits {
			if _, ok := fileCache[e.File]; ok {
				continue
			}
			lines, err := readLines(e.File)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", e.File, err)
			}
			fileCache[e.File] = lines
		}
	}

	seen := map[string]bool{} // deduplicate by "file:line"
	var ops []FixOp
	for _, f := range r.Findings {
		if f.Fix == nil {
			continue
		}
		for _, e := range f.Fix.Edits {
			key := fmt.Sprintf("%s:%d", e.File, e.Line)
			if seen[key] {
				continue
			}
			seen[key] = true

			lines := fileCache[e.File]
			if e.Line < 1 || e.Line > len(lines) {
				continue
			}
			orig := lines[e.Line-1]
			trimmed := strings.TrimSpace(orig)
			if strings.HasPrefix(trimmed, "#") {
				continue // already commented out — skip
			}

			// Preserve leading whitespace so indentation is unchanged.
			leading := orig[:len(orig)-len(strings.TrimLeft(orig, " \t"))]
			ops = append(ops, FixOp{
				File:    e.File,
				Line:    e.Line,
				OldLine: orig,
				NewLine: leading + "# envset: " + strings.TrimLeft(orig, " \t"),
			})
		}
	}

	// Sort deterministically: by file path, then line number ascending.
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].File != ops[j].File {
			return ops[i].File < ops[j].File
		}
		return ops[i].Line < ops[j].Line
	})

	return ops, nil
}

// PrintDiff writes a human-readable diff preview of all pending fix operations.
func PrintDiff(w io.Writer, ops []FixOp) {
	useColor := isTerminal(w)

	if len(ops) == 0 {
		fmt.Fprintln(w, color(useColor, colorGreen, "No auto-fixable issues found."))
		return
	}

	fmt.Fprintf(w, "\n%s\n\n",
		color(useColor, colorBold, fmt.Sprintf("Preview: %d auto-fix(es) to apply (comment-out only, backups created)", len(ops))),
	)

	// Group by file to produce a compact per-file diff block.
	byFile := map[string][]FixOp{}
	var fileOrder []string
	for _, op := range ops {
		if _, ok := byFile[op.File]; !ok {
			fileOrder = append(fileOrder, op.File)
		}
		byFile[op.File] = append(byFile[op.File], op)
	}

	for _, file := range fileOrder {
		fmt.Fprintf(w, "%s\n", color(useColor, colorBold, "--- "+file))
		for _, op := range byFile[file] {
			fmt.Fprintf(w, "  line %-4d  %s %s\n", op.Line, color(useColor, colorRed, "-"), op.OldLine)
			fmt.Fprintf(w, "            %s %s\n", color(useColor, colorGreen, "+"), op.NewLine)
		}
		fmt.Fprintln(w)
	}
}

// ApplyFixes applies all fix operations to disk and returns, per file, the
// backup of its previous contents (see `envset restore`).
//
// Every file is verified before anything is written: if a target line no
// longer matches what the audit saw (the file was edited in the meantime),
// nothing is written at all. Each file is then written atomically, after a
// shell syntax check, with all other bytes preserved exactly.
func ApplyFixes(ops []FixOp) (map[string]string, error) {
	if len(ops) == 0 {
		return nil, nil
	}

	byFile := map[string][]FixOp{}
	var fileOrder []string
	for _, op := range ops {
		if _, ok := byFile[op.File]; !ok {
			fileOrder = append(fileOrder, op.File)
		}
		byFile[op.File] = append(byFile[op.File], op)
	}

	// Lock every file before reading any, and hold the locks until all are written.
	unlock, err := fsutil.LockAll(fileOrder)
	if err != nil {
		return nil, err
	}
	defer unlock()

	type planned struct {
		raw, next []byte
	}
	plans := map[string]planned{}
	for _, file := range fileOrder {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}
		content := string(raw)
		trailingNL := strings.HasSuffix(content, "\n")
		lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
		for _, op := range byFile[file] {
			if op.Line < 1 || op.Line > len(lines) {
				return nil, fmt.Errorf("%s changed since the audit (line %d no longer exists); re-run `envset audit --fix`", file, op.Line)
			}
			cur := lines[op.Line-1]
			if strings.TrimSuffix(cur, "\r") != op.OldLine {
				return nil, fmt.Errorf("%s changed since the audit (line %d is different now); re-run `envset audit --fix`", file, op.Line)
			}
			if strings.HasSuffix(cur, "\r") {
				lines[op.Line-1] = op.NewLine + "\r"
			} else {
				lines[op.Line-1] = op.NewLine
			}
		}
		next := strings.Join(lines, "\n")
		if trailingNL {
			next += "\n"
		}
		plans[file] = planned{raw: raw, next: []byte(next)}
	}

	backups := map[string]string{}
	for _, file := range fileOrder {
		pl := plans[file]
		b, err := targets.CommitFile(file, targets.ShellForPath(file), pl.raw, true, pl.next, 0o644)
		if err != nil {
			return backups, fmt.Errorf("writing %s: %w", file, err)
		}
		backups[file] = b
	}
	return backups, nil
}

// CountFixable returns the number of findings in r that have an auto-fix.
func CountFixable(r Report) int {
	var n int
	for _, f := range r.Findings {
		if f.Fixable() {
			n++
		}
	}
	return n
}
