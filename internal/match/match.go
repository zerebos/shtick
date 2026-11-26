// Package match finds which aliases and functions a typed line used, and
// which ones it could have used.
package match

import (
	"sort"
	"strings"

	"github.com/zerebos/shtick/internal/defs"
	"github.com/zerebos/shtick/internal/shparse"
)

// EventKind is what happened.
type EventKind string

const (
	Used   EventKind = "used"
	Missed EventKind = "missed"
)

// Event is one use or missed use of a definition.
type Event struct {
	Kind EventKind
	Def  defs.Kind
	Name string
	// Expansion is the text the user typed instead (Missed only).
	Expansion string
}

type template struct {
	def    defs.Def
	cmds   [][]string // unquoted words per simple command
	prefix bool       // last command may have extra args
	words  int        // total template words, for specificity
}

// Matcher holds templates for a definition set. Parsing every body on every
// command is the slow part of the hook, so definitions are grouped by their
// first token and only the groups a typed line could hit get compiled.
type Matcher struct {
	set      *defs.Set
	sh       shparse.Dialect
	byFirst  map[string][]defs.Def
	compiled map[string][]template
	// byFold finds aliases and functions by lowercased name, for shells
	// whose command names are case-insensitive.
	aliasFold, funcFold map[string]defs.Def
}

// New indexes a definition set.
func New(set *defs.Set) *Matcher {
	m := &Matcher{
		set:      set,
		sh:       shparse.ForShell(string(set.Shell)),
		byFirst:  map[string][]defs.Def{},
		compiled: map[string][]template{},
	}
	for _, src := range []map[string]defs.Def{set.Aliases, set.Functions} {
		for _, d := range src {
			if f := m.fold(firstToken(d.Value)); f != "" {
				m.byFirst[f] = append(m.byFirst[f], d)
			}
		}
	}
	if m.sh.CaseInsensitive {
		m.aliasFold, m.funcFold = map[string]defs.Def{}, map[string]defs.Def{}
		for n, d := range set.Aliases {
			m.aliasFold[strings.ToLower(n)] = d
		}
		for n, d := range set.Functions {
			m.funcFold[strings.ToLower(n)] = d
		}
	}
	return m
}

// fold normalizes a command word for lookups.
func (m *Matcher) fold(w string) string {
	if m.sh.CaseInsensitive {
		return strings.ToLower(w)
	}
	return w
}

func (m *Matcher) alias(name string) (defs.Def, bool) {
	if m.aliasFold != nil {
		d, ok := m.aliasFold[strings.ToLower(name)]
		return d, ok
	}
	d, ok := m.set.Aliases[name]
	return d, ok
}

func (m *Matcher) function(name string) (defs.Def, bool) {
	if m.funcFold != nil {
		d, ok := m.funcFold[strings.ToLower(name)]
		return d, ok
	}
	d, ok := m.set.Functions[name]
	return d, ok
}

// firstToken is a cheap guess at a body's first word. It may be wrong for
// quoted first words, which then simply never match.
func firstToken(v string) string {
	v = strings.TrimLeft(v, " \t\n")
	if i := strings.IndexAny(v, " \t\n"); i >= 0 {
		return v[:i]
	}
	return v
}

func (m *Matcher) templatesFor(word string) []template {
	word = m.fold(word)
	if ts, ok := m.compiled[word]; ok {
		return ts
	}
	var ts []template
	for _, d := range m.byFirst[word] {
		var t template
		var ok bool
		if d.Kind == defs.Function {
			t, ok = FunctionTemplate(d)
		} else {
			t, ok = aliasTemplate(d)
		}
		if ok {
			ts = append(ts, t)
		}
	}
	// deterministic order for ties
	sort.Slice(ts, func(i, j int) bool { return ts[i].def.Name < ts[j].def.Name })
	m.compiled[word] = ts
	return ts
}

func aliasTemplate(d defs.Def) (template, bool) {
	sh := shparse.ForShell(string(d.Shell))
	cmds := toWords(sh, sh.Split(d.Value))
	if len(cmds) == 0 {
		return template{}, false
	}
	t := template{def: d, cmds: cmds, prefix: true, words: count(cmds)}
	// only worth a nudge if the alias is shorter than what it replaces
	if len(d.Name) >= len(strings.Join(flatten(cmds), " ")) {
		return template{}, false
	}
	return t, true
}

// FunctionTemplate turns a function body into a template if it is a plain
// sequence of commands, optionally ending with "$@". Bodies with control flow,
// local variables, or positional parameters other than a trailing "$@" are not
// templates. Exported for the defs command, which shows why a function is not
// nudgeable.
func FunctionTemplate(d defs.Def) (template, bool) {
	rules := rulesFor(d.Shell)
	parsed := rules.dialect.Split(d.Value)
	if len(parsed) == 0 {
		return template{}, false
	}
	t := template{def: d}
	for ci, c := range parsed {
		if len(c) == 0 {
			continue
		}
		if rules.isControl(c[0]) {
			return template{}, false
		}
		var words []string
		for wi, w := range c {
			last := ci == len(parsed)-1 && wi == len(c)-1
			if last && rules.argsTail.MatchString(w.Raw) {
				t.prefix = true
				continue
			}
			if rules.positional.MatchString(w.Raw) {
				return template{}, false
			}
			words = append(words, w.Text)
		}
		if len(words) == 0 {
			return template{}, false
		}
		t.cmds = append(t.cmds, words)
	}
	t.words = count(t.cmds)
	if len(t.cmds) == 0 {
		return template{}, false
	}
	return t, true
}

// Nudgeable reports whether a function body can be matched against typed lines.
func Nudgeable(d defs.Def) bool {
	_, ok := FunctionTemplate(d)
	return ok
}

// Line returns the events for one typed line.
func (m *Matcher) Line(line string) []Event {
	cmds := m.sh.Split(line)
	var events []Event
	// typed keeps every word as typed (modifiers included) so templates that
	// start with sudo or noglob match literally
	typed := make([][]string, len(cmds))
	usedCmd := make([]bool, len(cmds))

	for i, c := range cmds {
		for _, w := range c {
			if g, ok := m.set.Globals[w.Text]; ok && w.Raw == w.Text {
				events = append(events, Event{Kind: Used, Def: defs.Global, Name: g.Name})
			}
		}
		typed[i] = c.Texts()
		s := m.sh.Strip(c)
		if len(s) == 0 {
			continue
		}
		word := s[0]
		if word.Raw != word.Text || strings.HasPrefix(word.Raw, "\\") {
			continue // quoting a command word disables alias expansion
		}
		if d, ok := m.alias(word.Text); ok {
			events = append(events, Event{Kind: Used, Def: defs.Alias, Name: d.Name})
			usedCmd[i] = true
		} else if d, ok := m.function(word.Text); ok {
			events = append(events, Event{Kind: Used, Def: defs.Function, Name: d.Name})
			usedCmd[i] = true
		}
	}

	type cand struct {
		t          template
		start, end int // command span [start, end)
	}
	var cands []cand
	for start, words := range typed {
		if len(words) == 0 {
			continue
		}
		for _, t := range m.templatesFor(words[0]) {
			if start+len(t.cmds) <= len(typed) && m.matchAt(t, typed, usedCmd, start) {
				cands = append(cands, cand{t: t, start: start, end: start + len(t.cmds)})
			}
		}
	}
	// most specific first, then accept non-overlapping spans
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].t.words > cands[j].t.words })
	taken := make([]bool, len(typed))
	var missed []cand
	for _, c := range cands {
		free := true
		for k := c.start; k < c.end; k++ {
			if taken[k] {
				free = false
				break
			}
		}
		if !free {
			continue
		}
		for k := c.start; k < c.end; k++ {
			taken[k] = true
		}
		missed = append(missed, c)
	}
	sort.Slice(missed, func(i, j int) bool { return missed[i].start < missed[j].start })
	for _, c := range missed {
		events = append(events, Event{
			Kind:      Missed,
			Def:       c.t.def.Kind,
			Name:      c.t.def.Name,
			Expansion: expansion(c.t),
		})
	}
	return events
}

func (m *Matcher) matchAt(t template, typed [][]string, used []bool, start int) bool {
	for k, want := range t.cmds {
		i := start + k
		if used[i] {
			return false
		}
		got := typed[i]
		last := k == len(t.cmds)-1
		if len(got) < len(want) {
			return false
		}
		if !(last && t.prefix) && len(got) != len(want) {
			return false
		}
		for j := range want {
			if got[j] != want[j] && !(j == 0 && m.sh.CaseInsensitive && strings.EqualFold(got[j], want[j])) {
				return false
			}
		}
	}
	return true
}

func expansion(t template) string {
	parts := make([]string, len(t.cmds))
	for i, c := range t.cmds {
		parts[i] = strings.Join(c, " ")
	}
	return strings.Join(parts, "; ")
}

func toWords(sh shparse.Dialect, cmds []shparse.Command) [][]string {
	var out [][]string
	for _, c := range cmds {
		if s := sh.Strip(c); len(s) > 0 {
			out = append(out, c.Texts())
		}
	}
	return out
}

func count(cmds [][]string) int {
	n := 0
	for _, c := range cmds {
		n += len(c)
	}
	return n
}

func flatten(cmds [][]string) []string {
	var out []string
	for _, c := range cmds {
		out = append(out, c...)
	}
	return out
}
