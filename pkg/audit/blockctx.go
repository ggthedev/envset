package audit

import (
	"regexp"
	"strings"
)

// BlockContext tracks the enclosing block stack at each line of a shell file.
// It models two structures relevant to false-positive suppression in the
// slow-init detector:
//
//   - case ... esac depth (terminal-conditional execution)
//   - if ... fi predicate stack (cache-prime / guarded execution)
//
// Snapshots are taken AFTER closers and BEFORE openers on each line, so:
//
//   - The `if X; then` line is NOT inside the if it opens.
//   - Lines in the body ARE inside.
//   - The `fi` line is NOT inside the if it closes.
//
// The same convention applies to case/esac.
type BlockContext struct {
	caseDepth []int      // depth of enclosing case ... esac at each line (0-based index)
	ifPreds   [][]string // predicates of enclosing if blocks at each line, outermost first
}

var (
	reIfStart       = regexp.MustCompile(`^\s*if\s+(.*?)(?:\s*;\s*then\s*)?$`)
	reElif          = regexp.MustCompile(`^\s*elif\s+(.*?)(?:\s*;\s*then\s*)?$`)
	reFi            = regexp.MustCompile(`^\s*fi\b`)
	reCaseIn        = regexp.MustCompile(`^\s*case\s+.*\bin\s*$`)
	reEsac          = regexp.MustCompile(`^\s*esac\b`)
	reCachePathTest = regexp.MustCompile(`!\s*-[efds]\s+`)
)

// NewBlockContext analyzes lines (0-indexed) and returns a context that can
// answer "what blocks enclose line N?" in O(1) per query.
func NewBlockContext(lines []string) *BlockContext {
	ctx := &BlockContext{
		caseDepth: make([]int, len(lines)),
		ifPreds:   make([][]string, len(lines)),
	}

	caseDepth := 0
	var ifStack []string

	for i, raw := range lines {
		trim := strings.TrimSpace(raw)

		// Skip processing for comments — they affect nothing.
		isComment := strings.HasPrefix(trim, "#")

		// 1. Process closers first so the closer line itself isn't marked
		//    as inside the block it terminates.
		if !isComment {
			if reFi.MatchString(trim) && len(ifStack) > 0 {
				ifStack = ifStack[:len(ifStack)-1]
			}
			if reEsac.MatchString(trim) && caseDepth > 0 {
				caseDepth--
			}
		}

		// 2. Snapshot the state.
		ctx.caseDepth[i] = caseDepth
		snap := make([]string, len(ifStack))
		copy(snap, ifStack)
		ctx.ifPreds[i] = snap

		// 3. Process openers so subsequent lines see the new block.
		if !isComment {
			if m := reIfStart.FindStringSubmatch(trim); m != nil {
				ifStack = append(ifStack, m[1])
			} else if m := reElif.FindStringSubmatch(trim); m != nil {
				if len(ifStack) > 0 {
					ifStack[len(ifStack)-1] = m[1]
				}
			}
			if reCaseIn.MatchString(trim) {
				caseDepth++
			}
		}
	}

	return ctx
}

// InCase reports whether the 1-based line number is inside any case ... esac.
func (b *BlockContext) InCase(line int) bool {
	idx := line - 1
	if idx < 0 || idx >= len(b.caseDepth) {
		return false
	}
	return b.caseDepth[idx] > 0
}

// InCachePrimeGuard reports whether the 1-based line number is inside an `if`
// whose predicate looks like a cache-existence test (`! -f X`, `! -e X`, etc).
// This recognises the "generate once, source thereafter" idiom:
//
//	if [[ ! -f $cache ]]; then
//	    fnm env > $cache    # this is the slow init, runs once
//	fi
//	source $cache           # this is the per-shell cost
func (b *BlockContext) InCachePrimeGuard(line int) bool {
	idx := line - 1
	if idx < 0 || idx >= len(b.ifPreds) {
		return false
	}
	for _, pred := range b.ifPreds[idx] {
		if reCachePathTest.MatchString(pred) {
			return true
		}
	}
	return false
}
