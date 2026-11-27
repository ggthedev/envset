package targets

import (
	"fmt"
	"github.com/ggthedev/envset/pkg/logger"
	"strconv"
	"strings"
)

// This file is a deliberately small scanner for sh, bash, zsh and fish files.
// It is not a shell parser: it only tracks enough to know whether a line is
// code, whether it sits inside a block, and whether it holds exactly one
// statement. When unsure it errs towards "do not rewrite".

// lineInfo describes one line of a shell file.
type lineInfo struct {
	Inert     bool // heredoc text or the inside of a multi-line string: not code
	Depth     int  // blocks (if, case, loop, function, braces, parens) open at line start
	Continued bool // continues the previous line (trailing backslash, &&, || or |)

	Words     int  // top-level words
	HasOp     bool // a top-level ; & | < > or subshell paren
	Open      bool // leaves a quote, substitution or backslash open at the end
	CommentAt int  // byte offset of a trailing comment, -1 if none
}

const (
	inSingle = '\''
	inDouble = '"'
	inANSI   = 'a' // $'…'
	inTick   = '`'
	inParen  = '(' // subshell or $( )
	inArray  = 'A' // name=( … )
	inArith  = 'm' // (( ))
	inBrace  = '{' // ${ }
)

type heredoc struct {
	end       string
	stripTabs bool
}

// frame is an open quote, substitution or paren.
type frame struct {
	kind  byte
	cases int  // case blocks already open: a ")" beyond these is a pattern
	subst bool // $( ): what follows its ")" is still the same command
}

type scanner struct {
	shell     string
	fish      bool
	stack     []frame
	cases     int
	blocks    int
	docs      []heredoc // announced; bodies start on the next line
	continued bool
	pending   bool // the last code line ended in &&, || or |
	cmdPos    bool // the next word starts a command
	afterElse bool
}

func (s *scanner) top() byte {
	if len(s.stack) == 0 {
		return 0
	}
	return s.stack[len(s.stack)-1].kind
}
func (s *scanner) push(kind byte) { s.stack = append(s.stack, frame{kind: kind, cases: s.cases}) }
func (s *scanner) pushSubst() {
	s.stack = append(s.stack, frame{kind: inParen, cases: s.cases, subst: true})
}
func (s *scanner) pop() { s.stack = s.stack[:len(s.stack)-1] }

func (s *scanner) inString() bool {
	switch s.top() {
	case inSingle, inDouble, inANSI, inTick:
		return true
	}
	return false
}

func (s *scanner) depth() int {
	d := s.blocks
	for _, f := range s.stack {
		if f.kind == inParen || f.kind == inArray || f.kind == inArith {
			d++
		}
	}
	return d
}

// scanShell returns one lineInfo per line. shell is zsh, bash, sh or fish.
func scanShell(lines []string, shell string) []lineInfo {
	defer logger.Enter()()
	s := &scanner{shell: shell, fish: shell == "fish", cmdPos: true}
	out := make([]lineInfo, len(lines))
	for i, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		// A heredoc body starts after the whole command line, including
		// any backslash continuations of it.
		if len(s.docs) > 0 && !s.continued {
			out[i] = lineInfo{Inert: true, Depth: s.depth(), CommentAt: -1}
			body := line
			if s.docs[0].stripTabs {
				body = strings.TrimLeft(body, "\t")
			}
			if body == s.docs[0].end {
				s.docs = s.docs[1:]
			}
			continue
		}
		out[i] = s.line(line)
	}
	if logger.Verbose() { // line numbers and state only, never contents
		for i, in := range out {
			logger.Debug("scan line %d: depth=%d inert=%v continued=%v open=%v", i+1, in.Depth, in.Inert, in.Continued, in.Open)
		}
	}
	return out
}

func (s *scanner) line(line string) lineInfo {
	backslash := s.continued // the previous line ended in a backslash
	info := lineInfo{Inert: s.inString(), Depth: s.depth(), Continued: backslash || s.pending, CommentAt: -1}
	base := len(s.stack)
	s.continued = false
	joins := false // the latest top-level token is &&, || or |
	code := false  // the line has something other than blanks and a comment
	// A new line starts a command at top level and inside ( ) or $( ), unless
	// it continues the previous one (then the words are still arguments).
	if !backslash && (base == 0 || s.top() == inParen) {
		s.cmdPos = true
	}
	redirect := -2 // index of the latest unescaped ">"

	var word strings.Builder
	started := false
	add := func(c ...byte) { word.Write(c); started = true }
	end := func() {
		if !started {
			return
		}
		s.keyword(word.String())
		if len(s.stack) <= base {
			info.Words++
			joins, code = false, true
		}
		word.Reset()
		started = false
	}
	op := func() {
		if len(s.stack) <= base {
			info.HasOp = true
			joins, code = false, true
		}
	}

	n := len(line)
	at := func(i int) byte {
		if i < n {
			return line[i]
		}
		return 0
	}
	for i := 0; i < n; i++ {
		c := line[i]
		switch s.top() {
		case inSingle:
			add(c)
			if s.fish && c == '\\' && i+1 < n {
				i++
				add(line[i])
			} else if c == '\'' {
				s.pop()
			}
			continue
		case inANSI, inTick, inDouble:
			add(c)
			switch {
			case c == '\\' && i+1 < n:
				i++
				add(line[i])
			case s.top() == inANSI && c == '\'', s.top() == inTick && c == '`', s.top() == inDouble && c == '"':
				s.pop()
			case s.top() == inDouble && c == '`':
				s.push(inTick)
			case s.top() == inDouble && c == '$' && at(i+1) == '(':
				i++
				add('(')
				s.pushSubst()
				s.cmdPos = true
			case s.top() == inDouble && c == '$' && at(i+1) == '{':
				i++
				add('{')
				s.push(inBrace)
			}
			continue
		}

		// Code: top level, inside parens, arithmetic or ${ }.
		if c == '\\' {
			if i+1 >= n {
				s.continued = true
				break
			}
			add(c, line[i+1])
			i++
			continue
		}
		switch c {
		case '\'':
			add(c)
			s.push(inSingle)
		case '"':
			add(c)
			s.push(inDouble)
		case '`':
			add(c)
			s.push(inTick)
		case '$':
			add(c)
			switch at(i + 1) {
			case '\'':
				add('\'')
				s.push(inANSI)
				i++
			case '{':
				add('{')
				s.push(inBrace)
				i++
			case '(':
				add('(')
				i++
				if at(i+1) == '(' {
					add('(')
					i++
					s.push(inArith)
				} else {
					s.pushSubst()
					s.cmdPos = true
				}
			}
		case '}':
			if s.top() == inBrace {
				add(c)
				s.pop()
			} else {
				add(c)
			}
		case '#':
			if started || s.top() == inBrace || s.top() == inArith {
				add(c)
				continue
			}
			if len(s.stack) <= base {
				info.CommentAt = i
			}
			i = n
		case ' ', '\t':
			if s.top() == inBrace {
				add(c)
			} else {
				end()
			}
		case ';', '&', '|':
			end()
			op()
			switch {
			case c == ';' && (at(i+1) == ';' || at(i+1) == '&' || at(i+1) == '|'):
				i++ // case terminators ;; ;& ;|
			case c == '&' && at(i+1) == '&', c == '|' && (at(i+1) == '|' || at(i+1) == '&'):
				i++
				joins = len(s.stack) <= base
			case c == '&' && at(i+1) == '|' && !s.fish:
				i++ // zsh "&|": background and disown, not a pipe
			case c == '|' && redirect != i-1: // not the ">|" redirect
				joins = len(s.stack) <= base
			}
			s.cmdPos = true
		case '<':
			if s.top() != inArith && at(i+1) == '<' && at(i+2) != '<' && !s.fish {
				end()
				op()
				i = s.heredocStart(line, i+2) - 1
				continue
			}
			end()
			op()
			s.cmdPos = false // a file name follows
			for at(i+1) == '<' {
				i++
			}
		case '>':
			if s.top() == inArith {
				add(c)
				continue
			}
			end()
			op()
			s.cmdPos = false
			redirect = i
		case '(':
			switch {
			case strings.HasSuffix(word.String(), "="): // array: name=( … )
				add(c)
				s.push(inArray)
				s.cmdPos = false
			case at(i+1) == '(' && !started:
				op()
				i++
				s.push(inArith)
			default:
				end()
				op()
				s.push(inParen)
				s.cmdPos = true
			}
		case ')':
			switch {
			case s.top() == inArith && at(i+1) == ')':
				i++
				s.pop()
				if len(s.stack) >= base && started {
					add(')', ')')
				}
			case s.top() == inArray:
				if started {
					add(c)
				}
				end()
				s.pop()
				s.cmdPos = false
				joins, code = false, true
			case s.top() == inParen && s.cases <= s.stack[len(s.stack)-1].cases:
				if started {
					add(c)
				}
				end()
				subst := s.stack[len(s.stack)-1].subst
				s.pop()
				s.cmdPos = !subst
				joins, code = false, true
			case s.top() == inArith:
				add(c)
			default: // a case pattern: "a)"
				end()
				s.cmdPos = true
			}
		default:
			add(c)
		}
	}
	end()
	if code { // blank and comment lines keep a pending && || | alive
		s.pending = joins
	}
	info.Open = s.continued || joins || len(s.stack) > base
	return info
}

// keyword updates the block depth for a completed word.
func (s *scanner) keyword(w string) {
	cmd := s.cmdPos
	s.cmdPos = false
	if s.top() == inArray {
		return // array elements are data
	}
	open := func() { s.blocks++ }
	closeBlock := func() {
		if s.blocks > 0 {
			s.blocks--
		}
	}
	if s.fish {
		afterElse := s.afterElse
		s.afterElse = w == "else"
		if !cmd {
			return
		}
		switch w {
		case "if":
			if !afterElse { // "else if" continues the same block
				open()
			}
			s.cmdPos = true
		case "function", "for", "switch", "begin":
			open()
			s.cmdPos = w == "begin"
		case "while":
			open()
			s.cmdPos = true
		case "end":
			closeBlock()
		case "else", "and", "or", "not":
			s.cmdPos = true
		}
		return
	}
	switch w {
	case "{": // function body or group, wherever it appears
		open()
		s.cmdPos = true
		return
	case "}":
		// zsh closes a block wherever "}" stands alone; bash and sh only where
		// a command could start, elsewhere it is an argument.
		if cmd || s.shell == "zsh" {
			closeBlock()
			s.cmdPos = true
		}
		return
	}
	if !cmd {
		return
	}
	switch w {
	case "if", "case", "do":
		open()
		if w == "case" {
			s.cases++
		}
		s.cmdPos = w != "case"
	case "fi", "esac", "done":
		if w == "esac" && s.cases > 0 {
			s.cases--
		}
		closeBlock()
		s.cmdPos = true // another closer may follow without ";"
	case "then", "else", "elif", "while", "until", "!", "time":
		s.cmdPos = true
	}
}

// heredocStart records the delimiter after "<<" and returns the index after it.
func (s *scanner) heredocStart(line string, i int) int {
	defer logger.Enter()()
	d := heredoc{}
	if i < len(line) && line[i] == '-' {
		d.stripTabs = true
		i++
	}
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	var b strings.Builder
	var quote byte
	for ; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				b.WriteByte(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
		} else if c == '\\' {
			continue
		} else if strings.IndexByte(" \t;&|<>()", c) >= 0 {
			break
		} else {
			b.WriteByte(c)
		}
	}
	if b.Len() > 0 {
		d.end = b.String()
		s.docs = append(s.docs, d)
	}
	return i
}

// entries parses every line and marks where each one sits.
func (s *ShellTarget) entries(lines []string) []Entry {
	defer logger.Enter()()
	infos := scanShell(lines, s.shell)
	out := make([]Entry, len(lines))
	for i, line := range lines {
		e := s.parseLine(line, i+1)
		in := infos[i]
		e.Inert = in.Inert
		e.Block = in.Depth > 0
		// A rewritable definition is one statement: "export NAME=value" (or
		// fish "set -x NAME values…"), optionally followed by a comment.
		wordsOK := in.Words == 2
		if e.VarType == VarTypeFishSet {
			wordsOK = in.Words >= 3
		}
		e.Compound = in.Continued || in.Open || in.HasOp || !wordsOK
		if in.CommentAt >= 0 && !e.IsComment {
			head := strings.TrimRight(line[:in.CommentAt], " \t")
			e.Comment = strings.TrimSuffix(line[len(head):], "\r")
		}
		out[i] = e
	}
	return out
}

// RefusedError reports lines envset will not rewrite because doing so could
// damage them or have no effect. Its message carries line numbers only, never
// file contents, because errors are logged.
type RefusedError struct {
	Path, Key string
	Entries   []Entry
	CanAppend bool
}

func (e *RefusedError) Error() string {
	var nums []string
	block, compound := false, false
	for _, en := range e.Entries {
		nums = append(nums, strconv.Itoa(en.LineNum))
		block = block || en.Block
		compound = compound || en.Compound
	}
	var why []string
	if block {
		why = append(why, "set inside a block (if, case, loop or function)")
	}
	if compound {
		why = append(why, "on a line that holds more than this one definition or continues another")
	}
	msg := fmt.Sprintf("not changing %s in %s: it is %s (line %s); edit it by hand",
		e.Key, e.Path, strings.Join(why, " and "), strings.Join(nums, ", "))
	if e.CanAppend {
		msg += ", or pass --append to add a new line at the end that overrides it"
	}
	return msg
}
