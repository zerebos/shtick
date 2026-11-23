// Package shparse splits a zsh command line into simple commands and words.
//
// It is not a full shell parser. It understands enough quoting, command
// substitution, and command separators to find the words a user typed for
// each simple command, which is all shtick needs.
package shparse

import (
	"strings"
	"unicode"
)

// Word is one shell word. Raw keeps the quotes as typed; Text has them removed.
type Word struct {
	Raw  string
	Text string
}

// Command is one simple command: the words between separators.
type Command []Word

// Texts returns the unquoted text of each word.
func (c Command) Texts() []string {
	out := make([]string, len(c))
	for i, w := range c {
		out[i] = w.Text
	}
	return out
}

// String joins the unquoted words with single spaces.
func (c Command) String() string {
	return strings.Join(c.Texts(), " ")
}

// Split breaks a line into simple commands. Separators are |, ||, &, &&, ;,
// newlines, and top-level parentheses. Everything inside quotes, $( ), <( ),
// and backticks stays part of the current word. A # at the start of a word
// starts a comment that runs to the end of the line.
func Split(line string) []Command {
	var (
		cmds    []Command
		cur     Command
		raw     strings.Builder
		text    strings.Builder
		inWord  bool
		single  bool
		double  bool
		escape  bool
		depth   int // $( ), <( ), >( ), =( ) nesting
		backtik bool
		comment bool
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
		if comment {
			if r == '\n' {
				comment = false
				flushCmd()
			}
			continue
		}
		if escape {
			escape = false
			raw.WriteRune(r)
			text.WriteRune(r)
			inWord = true
			continue
		}
		switch {
		case r == '\\' && !single && i+1 < len(rs) && rs[i+1] == '\n':
			i++ // line continuation
			continue
		case r == '\\' && !single:
			escape = true
			raw.WriteRune(r)
			inWord = true
			continue
		case single:
			raw.WriteRune(r)
			if r == '\'' {
				single = false
			} else {
				text.WriteRune(r)
			}
			continue
		case backtik:
			raw.WriteRune(r)
			text.WriteRune(r)
			if r == '`' {
				backtik = false
			}
			continue
		case depth > 0:
			raw.WriteRune(r)
			text.WriteRune(r)
			switch r {
			case '(':
				depth++
			case ')':
				depth--
			}
			continue
		case double:
			raw.WriteRune(r)
			switch r {
			case '"':
				double = false
			case '`':
				// backticks inside double quotes: keep simple, treat as text
				text.WriteRune(r)
			case '(':
				text.WriteRune(r)
				if i > 0 && rs[i-1] == '$' {
					depth++
				}
			default:
				text.WriteRune(r)
			}
			continue
		}

		// unquoted, top level
		switch {
		case r == '\'':
			single = true
			raw.WriteRune(r)
			inWord = true
		case r == '"':
			double = true
			raw.WriteRune(r)
			inWord = true
		case r == '`':
			backtik = true
			raw.WriteRune(r)
			text.WriteRune(r)
			inWord = true
		case r == '(':
			if i > 0 && strings.ContainsRune("$<>=", rs[i-1]) && inWord {
				depth++
				raw.WriteRune(r)
				text.WriteRune(r)
			} else {
				flushCmd() // subshell or function definition parens
			}
		case r == ')':
			flushCmd()
		case r == '#' && !inWord:
			comment = true
			flushWord()
		case r == '|' || r == ';' || r == '&' || r == '\n':
			// a trailing & on a redirection like >&2 or &> stays in the word
			if r == '&' && inWord && i > 0 && (rs[i-1] == '>' || rs[i-1] == '<') {
				raw.WriteRune(r)
				text.WriteRune(r)
				continue
			}
			if r == '&' && i+1 < len(rs) && rs[i+1] == '>' {
				flushWord()
				raw.WriteRune(r)
				text.WriteRune(r)
				inWord = true
				continue
			}
			flushCmd()
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

// precommand modifiers and keywords that come before the real command word.
var leaders = map[string]bool{
	"noglob": true, "nocorrect": true, "command": true, "builtin": true,
	"exec": true, "time": true, "sudo": true, "doas": true, "nohup": true,
	"!": true, "{": true, "}": true, "if": true, "then": true, "else": true,
	"elif": true, "do": true, "while": true, "until": true,
}

// compound starters whose following words are not a command.
var compound = map[string]bool{
	"for": true, "foreach": true, "case": true, "select": true,
	"function": true, "repeat": true, "fi": true, "done": true, "esac": true,
}

// Strip removes leading assignments, precommand modifiers, and keywords so
// the first word is the command word. It returns nil for compound commands
// like for loops whose next word is not a command. Flags given to sudo or
// doas are skipped too.
func Strip(c Command) Command {
	i := 0
	for i < len(c) {
		t := c[i].Text
		switch {
		case compound[t]:
			return nil
		case leaders[t]:
			i++
			if t == "sudo" || t == "doas" {
				for i < len(c) && strings.HasPrefix(c[i].Text, "-") {
					i++
				}
			}
		case isAssignment(c[i].Raw):
			i++
		default:
			return c[i:]
		}
	}
	return nil
}

func isAssignment(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	name := strings.TrimSuffix(w[:eq], "+")
	if br := strings.IndexByte(name, '['); br > 0 && strings.HasSuffix(name, "]") {
		name = name[:br]
	}
	for i, r := range name {
		if r != '_' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
			return false
		}
	}
	return name != ""
}
