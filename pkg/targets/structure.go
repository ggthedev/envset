package targets

import (
	"fmt"
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
	Continued bool // continues the previous line (trailing backslash)

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
	inParen  = '(' // subshell, $( ), array
	inArith  = 'm' // (( ))
	inBrace  = '{' // ${ }
)

type heredoc struct {
	end       string
	stripTabs bool
}

type scanner struct {
	fish      bool
	stack     []byte
	blocks    int
	docs      []heredoc // announced; bodies start on the next line
	continued bool
	cmdPos    bool // the next word starts a command
	afterElse bool
}

func (s *scanner) top() byte {
	if len(s.stack) == 0 {
		return 0
	}
	return s.stack[len(s.stack)-1]
}
func (s *scanner) push(f byte) { s.stack = append(s.stack, f) }
func (s *scanner) pop()        { s.stack = s.stack[:len(s.stack)-1] }

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
		if f == inParen || f == inArith {
			d++
		}
	}
	return d
}

// scanShell returns one lineInfo per line.
func scanShell(lines []string, fish bool) []lineInfo {
	s := &scanner{fish: fish, cmdPos: true}
	out := make([]lineInfo, len(lines))
	for i, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		if len(s.docs) > 0 {
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
	return out
}

func (s *scanner) line(line string) lineInfo {
	info := lineInfo{Inert: s.inString(), Depth: s.depth(), Continued: s.continued, CommentAt: -1}
	base := len(s.stack)
	s.continued = false
	if base == 0 && !info.Continued {
		s.cmdPos = true
	}

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
		}
		word.Reset()
		started = false
	}
	op := func() {
		if len(s.stack) <= base {
			info.HasOp = true
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
				s.push(inParen)
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
					s.push(inParen)
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
		case '(':
			switch {
			case strings.HasSuffix(word.String(), "="): // array: name=( … )
				add(c)
				s.push(inParen)
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
			case s.top() == inParen:
				if started {
					add(c)
				}
				end()
				s.pop()
				s.cmdPos = true
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
	info.Open = s.continued || len(s.stack) > base
	return info
}

// keyword updates the block depth for a completed word.
func (s *scanner) keyword(w string) {
	cmd := s.cmdPos
	s.cmdPos = false
	open := func() { s.blocks++ }
	close := func() {
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
			close()
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
		close()
		return
	}
	if !cmd {
		return
	}
	switch w {
	case "if", "case", "do":
		open()
		s.cmdPos = w != "case"
	case "fi", "esac", "done":
		close()
	case "then", "else", "elif", "while", "until", "!", "time":
		s.cmdPos = true
	}
}

// heredocStart records the delimiter after "<<" and returns the index after it.
func (s *scanner) heredocStart(line string, i int) int {
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
	infos := scanShell(lines, s.shell == "fish")
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
		why = append(why, "on a line that holds more than this one definition")
	}
	msg := fmt.Sprintf("not changing %s in %s: it is %s (line %s); edit it by hand",
		e.Key, e.Path, strings.Join(why, " and "), strings.Join(nums, ", "))
	if e.CanAppend {
		msg += ", or pass --append to add a new line at the end that overrides it"
	}
	return msg
}
