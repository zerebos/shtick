// Package suggest finds commands you type often that have no alias or
// function, and proposes a short name for each.
package suggest

import (
	"sort"
	"strings"

	"github.com/zerebos/shtick/internal/defs"
	"github.com/zerebos/shtick/internal/match"
	"github.com/zerebos/shtick/internal/shparse"
)

// Suggestion is a candidate alias.
type Suggestion struct {
	Command string
	Count   int
	Name    string
}

// Options tune the search.
type Options struct {
	MinCount int                    // ignore prefixes seen fewer times
	MinChars int                    // ignore prefixes shorter than this
	Limit    int                    // max suggestions
	Taken    func(name string) bool // reports names in use (PATH, builtins)
}

// Find scans history lines and returns candidates, best first. A candidate is
// the first two or three words of a simple command. Commands that already
// start with an alias or function, or that an alias or function would cover,
// are left out.
func Find(lines []string, set *defs.Set, opt Options) []Suggestion {
	if opt.MinCount <= 0 {
		opt.MinCount = 10
	}
	if opt.MinChars <= 0 {
		opt.MinChars = 8
	}
	if opt.Limit <= 0 {
		opt.Limit = 10
	}
	m := match.New(set)
	sh := shparse.ForShell(string(set.Shell))
	counts := map[string]int{}
	for _, line := range lines {
		covered := map[string]bool{}
		for _, e := range m.Line(line) {
			if e.Kind == match.Missed {
				covered[e.Expansion] = true
			}
		}
		for _, c := range sh.Split(line) {
			s := sh.Strip(c)
			if len(s) < 2 || set.Has(s[0].Text) {
				continue
			}
			words := s.Texts()
			for n := 2; n <= 3 && n <= len(words); n++ {
				if !plain(s[:n]) {
					break
				}
				key := strings.Join(words[:n], " ")
				if coveredBy(key, covered) {
					continue
				}
				counts[key]++
			}
		}
	}

	var out []Suggestion
	for cmd, n := range counts {
		if n < opt.MinCount || len(cmd) < opt.MinChars {
			continue
		}
		out = append(out, Suggestion{Command: cmd, Count: n})
	}
	// rank by keystrokes saved, roughly count times length
	sort.Slice(out, func(i, j int) bool {
		si, sj := out[i].Count*len(out[i].Command), out[j].Count*len(out[j].Command)
		if si != sj {
			return si > sj
		}
		return out[i].Command < out[j].Command
	})
	out = dropShadowed(out)
	if len(out) > opt.Limit {
		out = out[:opt.Limit]
	}
	used := map[string]bool{}
	for i := range out {
		cmd := out[i].Command
		if sh.CaseInsensitive {
			cmd = strings.ToLower(cmd) // Get-ChildItem -Recurse -> gr, not GR
		}
		out[i].Name = name(cmd, func(s string) bool {
			return used[s] || set.Defined(s) || (opt.Taken != nil && opt.Taken(s))
		})
		used[out[i].Name] = true
	}
	return out
}

// plain words only: no quotes, expansions, globs, paths, or redirections.
func plain(ws shparse.Command) bool {
	for _, w := range ws {
		if w.Raw != w.Text || strings.ContainsAny(w.Text, "$`*?[](){}<>|&;/=~\"'\\@") || w.Text == "" {
			return false
		}
	}
	return true
}

func coveredBy(key string, covered map[string]bool) bool {
	for exp := range covered {
		if strings.HasPrefix(exp, key) || strings.HasPrefix(key, exp) {
			return true
		}
	}
	return false
}

// dropShadowed removes a two word prefix when its three word extension is
// nearly as common, so "docker compose" doesn't crowd out "docker compose up".
func dropShadowed(in []Suggestion) []Suggestion {
	byCmd := map[string]int{}
	for _, s := range in {
		byCmd[s.Command] = s.Count
	}
	var out []Suggestion
	for _, s := range in {
		shadowed := false
		for cmd, n := range byCmd {
			if cmd != s.Command && strings.HasPrefix(cmd, s.Command+" ") && n*10 >= s.Count*8 {
				shadowed = true
				break
			}
		}
		if !shadowed {
			out = append(out, s)
		}
	}
	return out
}

// name builds initials (git status -> gs) and lengthens them until free.
func name(cmd string, taken func(string) bool) string {
	words := strings.Fields(cmd)
	var parts []string
	for _, w := range words {
		parts = append(parts, strings.TrimLeft(w, "-"))
	}
	base := ""
	for _, p := range parts {
		if p != "" {
			base += p[:1]
		}
	}
	if !taken(base) {
		return base
	}
	// grow using more letters of the last word: gs, gst, gsta
	last := parts[len(parts)-1]
	prefix := base[:len(base)-1]
	for n := 2; n <= len(last); n++ {
		c := prefix + last[:n]
		if !taken(c) {
			return c
		}
	}
	for i := 2; i <= 9; i++ {
		c := base + string(rune('0'+i))
		if !taken(c) {
			return c
		}
	}
	return base
}
