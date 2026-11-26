package shparse

import (
	"strings"
	"unicode"
)

// Dialect is how one shell splits a line and finds the command word.
type Dialect struct {
	Split func(string) []Command
	Strip func(Command) Command
	// CaseInsensitive shells match command names ignoring case.
	CaseInsensitive bool
}

var (
	Zsh  = Dialect{Split: Split, Strip: Strip}
	Pwsh = Dialect{Split: SplitPwsh, Strip: StripPwsh, CaseInsensitive: true}
)

// ForShell returns the dialect for a shell name. Unknown names get zsh.
func ForShell(shell string) Dialect {
	if shell == "pwsh" {
		return Pwsh
	}
	return Zsh
}

func isSingleQuote(r rune) bool {
	return r == '\'' || r == '‘' || r == '’' || r == '‚' || r == '‛'
}

func isDoubleQuote(r rune) bool {
	return r == '"' || r == '“' || r == '”' || r == '„'
}

// SplitPwsh breaks a PowerShell line into simple commands. Separators are |,
// ||, &&, ;, newlines, and a background &. A leading & (the call operator)
// stays as a word for StripPwsh to remove. The backtick escapes the next
// character, and a backtick before a newline continues the line. Groups like
// ( ), $( ), @( ), { }, and @{ } stay inside the current word, so a script
// block argument is one word. # comments and <# #> block comments are dropped.
func SplitPwsh(line string) []Command {
	var (
		cmds   []Command
		cur    Command
		raw    strings.Builder
		text   strings.Builder
		inWord bool
	)
	flushWord := func() {
		if inWord {
			cur = append(cur, Word{Raw: raw.String(), Text: text.String()})
		}
		raw.Reset()
		text.Reset()
		inWord = false
	}
	flushCmd := func() {
		flushWord()
		if len(cur) > 0 {
			cmds = append(cmds, cur)
		}
		cur = nil
	}

	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '`':
			if j := continuation(rs, i); j > i {
				i = j
				continue
			}
			raw.WriteRune(r)
			if i+1 < len(rs) {
				i++
				raw.WriteRune(rs[i])
				text.WriteRune(rs[i])
			}
			inWord = true
		case isSingleQuote(r):
			end, t := scanSingle(rs, i)
			raw.WriteString(string(rs[i:min(end+1, len(rs))]))
			text.WriteString(t)
			i = end
			inWord = true
		case isDoubleQuote(r):
			end, t := scanDouble(rs, i)
			raw.WriteString(string(rs[i:min(end+1, len(rs))]))
			text.WriteString(t)
			i = end
			inWord = true
		case r == '(' || r == '{':
			end := scanGroup(rs, i)
			g := string(rs[i:min(end+1, len(rs))])
			raw.WriteString(g)
			text.WriteString(g)
			i = end
			inWord = true
		case r == '<' && i+1 < len(rs) && rs[i+1] == '#':
			i = blockCommentEnd(rs, i)
			flushWord()
		case r == '#' && !inWord:
			for i+1 < len(rs) && rs[i+1] != '\n' {
				i++
			}
		case r == '\n' || r == ';' || r == '|':
			flushCmd()
		case r == '&':
			// 2>&1 and *>&1 keep the & in the redirection word
			if inWord && i > 0 && rs[i-1] == '>' {
				raw.WriteRune(r)
				text.WriteRune(r)
				continue
			}
			if i+1 < len(rs) && rs[i+1] == '&' {
				i++
				flushCmd()
				continue
			}
			flushWord()
			if len(cur) == 0 {
				cur = append(cur, Word{Raw: "&", Text: "&"}) // call operator
			} else {
				flushCmd() // background job
			}
		case unicode.IsSpace(r):
			flushWord()
		default:
			raw.WriteRune(r)
			text.WriteRune(r)
			inWord = true
		}
	}
	flushCmd()
	return cmds
}

// continuation returns the index of the newline when rs[i] is a backtick
// that continues the line (optionally through a \r), or i otherwise.
func continuation(rs []rune, i int) int {
	j := i + 1
	if j < len(rs) && rs[j] == '\r' {
		j++
	}
	if j < len(rs) && rs[j] == '\n' {
		return j
	}
	return i
}

// scanSingle reads a single quoted string starting at rs[i]. It returns the
// index of the closing quote (or the last rune) and the unquoted text. Two
// quotes in a row are one literal quote.
func scanSingle(rs []rune, i int) (int, string) {
	var t strings.Builder
	for j := i + 1; j < len(rs); j++ {
		if isSingleQuote(rs[j]) {
			if j+1 < len(rs) && isSingleQuote(rs[j+1]) {
				t.WriteRune(rs[j])
				j++
				continue
			}
			return j, t.String()
		}
		t.WriteRune(rs[j])
	}
	return len(rs) - 1, t.String()
}

// scanDouble reads a double quoted string starting at rs[i]. Backtick escapes
// keep the escaped rune, "" is one literal quote, and $( ) subexpressions are
// copied whole.
func scanDouble(rs []rune, i int) (int, string) {
	var t strings.Builder
	for j := i + 1; j < len(rs); j++ {
		r := rs[j]
		switch {
		case r == '`' && j+1 < len(rs):
			j++
			t.WriteRune(rs[j])
		case isDoubleQuote(r):
			if j+1 < len(rs) && isDoubleQuote(rs[j+1]) {
				t.WriteRune(r)
				j++
				continue
			}
			return j, t.String()
		case r == '$' && j+1 < len(rs) && rs[j+1] == '(':
			end := scanGroup(rs, j+1)
			t.WriteString(string(rs[j:min(end+1, len(rs))]))
			j = end
		default:
			t.WriteRune(r)
		}
	}
	return len(rs) - 1, t.String()
}

// scanGroup returns the index of the bracket that closes the ( or { at rs[i],
// skipping strings and comments inside it, or the last index when unclosed.
func scanGroup(rs []rune, i int) int {
	var stack []rune
	for j := i; j < len(rs); j++ {
		r := rs[j]
		switch {
		case r == '(':
			stack = append(stack, ')')
		case r == '{':
			stack = append(stack, '}')
		case r == ')' || r == '}':
			if len(stack) > 0 && stack[len(stack)-1] == r {
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return j
				}
			}
		case r == '`':
			j++
		case isSingleQuote(r):
			j, _ = scanSingle(rs, j)
		case isDoubleQuote(r):
			j, _ = scanDouble(rs, j)
		case r == '<' && j+1 < len(rs) && rs[j+1] == '#':
			j = blockCommentEnd(rs, j)
		case r == '#' && (j == 0 || unicode.IsSpace(rs[j-1]) || strings.ContainsRune("({;", rs[j-1])):
			for j+1 < len(rs) && rs[j+1] != '\n' {
				j++
			}
		}
	}
	return len(rs) - 1
}

// blockCommentEnd returns the index of the > that ends the <# at rs[i].
func blockCommentEnd(rs []rune, i int) int {
	for j := i + 2; j+1 < len(rs); j++ {
		if rs[j] == '#' && rs[j+1] == '>' {
			return j + 1
		}
	}
	return len(rs) - 1
}

// pwshLeaders come before the real command word: the call and dot-source
// operators, and sudo.
var pwshLeaders = map[string]bool{"&": true, ".": true, "sudo": true}

// pwshCompound are keywords whose following words are not a command word.
var pwshCompound = map[string]bool{
	"if": true, "elseif": true, "else": true, "for": true, "foreach": true,
	"while": true, "do": true, "until": true, "switch": true, "try": true,
	"catch": true, "finally": true, "trap": true, "throw": true, "return": true,
	"break": true, "continue": true, "exit": true, "function": true,
	"filter": true, "workflow": true, "configuration": true, "param": true,
	"begin": true, "process": true, "end": true, "clean": true,
	"dynamicparam": true, "class": true, "enum": true, "using": true, "data": true,
}

// PwshKeyword returns the lowercased word up to any ( or { that follows it
// without a space, since if($x) and param($a) tokenize as one word.
func PwshKeyword(w Word) string {
	t := w.Text
	if i := strings.IndexAny(t, "({"); i > 0 {
		t = t[:i]
	}
	return strings.ToLower(t)
}

var assignOps = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "??=": true,
}

// StripPwsh removes the call operator, dot-sourcing, sudo (with its flags),
// and a leading variable assignment, so the first word is the command word.
// It returns nil for keywords like foreach whose next word is not a command,
// and for assignments written without spaces ($x=git).
func StripPwsh(c Command) Command {
	i := 0
	for i < len(c) {
		w := c[i]
		t := strings.ToLower(w.Text)
		bare := w.Raw == w.Text
		switch {
		case bare && pwshCompound[PwshKeyword(w)]:
			return nil
		case bare && pwshLeaders[t]:
			i++
			if t == "sudo" {
				for i < len(c) && strings.HasPrefix(c[i].Text, "-") {
					i++
				}
			}
		case strings.HasPrefix(w.Raw, "$") && i+1 < len(c) && assignOps[c[i+1].Text]:
			i += 2
		case strings.HasPrefix(w.Raw, "$") && strings.Contains(w.Raw, "="):
			return nil
		default:
			return c[i:]
		}
	}
	return nil
}
