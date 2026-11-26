// Package defs reads the alias and function dump the shell plugins write and
// decides which entries shtick tracks.
package defs

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
)

// Kind is the kind of definition.
type Kind string

const (
	Alias    Kind = "alias"
	Global   Kind = "global"
	Function Kind = "function"
)

// Shell is the shell a dump came from. A dump may start with a record of
// kind "shell" naming it; dumps without one are zsh.
type Shell string

const (
	Zsh  Shell = "zsh"
	Bash Shell = "bash"
	Pwsh Shell = "pwsh"
)

// shellKind is the record kind that names the shell. It is not a definition.
const shellKind = "shell"

// Def is one alias, global alias, or function.
type Def struct {
	Kind   Kind
	Name   string
	Source string // file a function was defined in, if the shell knows it
	Value  string // alias value or function body
	Shell  Shell  // shell the definition came from
}

// Skipped is a definition shtick ignores, with the reason.
type Skipped struct {
	Def
	Reason string
}

// Set holds the tracked definitions.
type Set struct {
	Shell     Shell
	Aliases   map[string]Def
	Globals   map[string]Def
	Functions map[string]Def
	Skipped   []Skipped

	// folded holds lowercased tracked names for shells whose command names
	// are case-insensitive, and folded skipped names under a "\x00" prefix.
	folded map[string]bool
}

// CaseInsensitive reports whether the shell ignores case in command names.
func (sh Shell) CaseInsensitive() bool { return sh == Pwsh }

// Has reports whether name is a tracked alias or function (not global).
func (s *Set) Has(name string) bool {
	if s.folded != nil {
		return s.folded[strings.ToLower(name)]
	}
	_, a := s.Aliases[name]
	_, f := s.Functions[name]
	return a || f
}

// Defined reports whether name is any alias or function, tracked or skipped.
// Used to avoid suggesting a name that is already taken.
func (s *Set) Defined(name string) bool {
	if s.Has(name) {
		return true
	}
	if s.folded != nil {
		return s.folded["\x00"+strings.ToLower(name)]
	}
	if _, ok := s.Globals[name]; ok {
		return true
	}
	for _, sk := range s.Skipped {
		if sk.Name == name {
			return true
		}
	}
	return false
}

// All returns every tracked definition sorted by name.
func (s *Set) All() []Def {
	var out []Def
	for _, m := range []map[string]Def{s.Aliases, s.Globals, s.Functions} {
		for _, d := range m {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// Lookup finds a tracked definition by kind and name.
func (s *Set) Lookup(kind Kind, name string) (Def, bool) {
	var m map[string]Def
	switch kind {
	case Alias:
		m = s.Aliases
	case Global:
		m = s.Globals
	case Function:
		m = s.Functions
	}
	d, ok := m[name]
	return d, ok
}

// Options control filtering.
type Options struct {
	IgnoreNames   []string // extra name globs to skip
	IgnoreSources []string // source path globs to skip; nil means DefaultIgnoreSources
}

// DefaultIgnoreSources are framework and system paths whose functions are not
// the user's own.
var DefaultIgnoreSources = []string{
	"*/.oh-my-zsh/*", "*/oh-my-zsh/*", "*/ohmyzsh/*",
	"*/zinit/*", "*/.zinit/*", "*/antidote/*", "*/.antidote/*",
	"*/.zim/*", "*/zim/*", "*/zplug/*", "*/.zplug/*", "*/.zprezto/*",
	"*/site-functions/*", "/usr/share/*", "/usr/local/share/*",
	"/opt/homebrew/share/*", "*/zsh/functions/*",
	"*/bash-completion/*", "*/bash_completion.d/*", "/etc/bash_completion",
	"*/.oh-my-bash/*", "*/oh-my-bash/*", "*/bash-it/*", "*/.bash_it/*",
	"/etc/profile.d/*",
}

var hookNames = map[string]bool{
	"precmd": true, "preexec": true, "chpwd": true, "periodic": true,
	"zshaddhistory": true, "zshexit": true, "zsh_directory_name": true,
	"command_not_found_handler": true, "command_not_found_handle": true,
}

// Parse reads a NUL separated dump: kind, name, source, value, repeated.
func Parse(data []byte, opt Options) (*Set, error) {
	fields := bytes.Split(data, []byte{0})
	// a well formed dump ends with a NUL, which leaves one empty trailing field
	if n := len(fields); n > 0 && len(fields[n-1]) == 0 {
		fields = fields[:n-1]
	}
	if len(fields)%4 != 0 {
		return nil, fmt.Errorf("defs: %d fields, want a multiple of 4", len(fields))
	}
	s := &Set{
		Shell:     Zsh,
		Aliases:   map[string]Def{},
		Globals:   map[string]Def{},
		Functions: map[string]Def{},
	}
	for i := 0; i < len(fields); i += 4 {
		if string(fields[i]) == shellKind {
			s.Shell = Shell(fields[i+1])
			break
		}
	}
	srcPatterns := opt.IgnoreSources
	if srcPatterns == nil {
		srcPatterns = DefaultIgnoreSources
		if s.Shell == Pwsh {
			srcPatterns = DefaultPwshIgnoreSources
		}
	}
	srcGlobs := make([]glob, len(srcPatterns))
	for i, p := range srcPatterns {
		srcGlobs[i] = compileGlob(p)
	}
	srcCache := map[string]string{} // most functions share a handful of files
	for i := 0; i < len(fields); i += 4 {
		d := Def{
			Kind:   Kind(fields[i]),
			Name:   string(fields[i+1]),
			Source: string(fields[i+2]),
			Value:  string(fields[i+3]),
			Shell:  s.Shell,
		}
		if d.Kind == shellKind {
			continue
		}
		if d.Kind == Function && s.Shell == Bash {
			d = fromBash(d)
		}
		skip := skipReason
		if s.Shell == Pwsh {
			skip = pwshSkipReason
		}
		if reason := skip(d, opt.IgnoreNames, srcGlobs, srcCache); reason != "" {
			s.Skipped = append(s.Skipped, Skipped{Def: d, Reason: reason})
			continue
		}
		switch d.Kind {
		case Alias:
			s.Aliases[d.Name] = d
		case Global:
			s.Globals[d.Name] = d
		case Function:
			s.Functions[d.Name] = d
		default:
			s.Skipped = append(s.Skipped, Skipped{Def: d, Reason: "unknown kind"})
		}
	}
	sort.Slice(s.Skipped, func(i, j int) bool { return s.Skipped[i].Name < s.Skipped[j].Name })
	if s.Shell.CaseInsensitive() {
		s.folded = map[string]bool{}
		for _, m := range []map[string]Def{s.Aliases, s.Functions} {
			for n := range m {
				s.folded[strings.ToLower(n)] = true
			}
		}
		for n := range s.Globals {
			s.folded["\x00"+strings.ToLower(n)] = true
		}
		for _, sk := range s.Skipped {
			s.folded["\x00"+strings.ToLower(sk.Name)] = true
		}
	}
	return s, nil
}

// fromBash normalizes a function from a bash dump, which holds the output of
// `declare -F name` with extdebug on ("name line file") as the source and
// `declare -f name` ("name () \n{ \n    body\n}") as the value. Bodies are
// unwrapped to match what zsh's $functions holds. Functions with redirections
// after the closing brace keep their wrapper, which makes them not
// nudgeable.
func fromBash(d Def) Def {
	if rest, ok := strings.CutPrefix(d.Source, d.Name+" "); ok && strings.HasSuffix(rest, "\n") {
		_, file, _ := strings.Cut(strings.TrimSuffix(rest, "\n"), " ")
		switch file {
		case "main", "environment": // typed at the prompt or inherited
			file = ""
		}
		d.Source = file
	}
	body, ok := strings.CutPrefix(d.Value, d.Name+" () \n{ \n")
	if !ok {
		return d
	}
	body = strings.TrimRight(body, "\n")
	if body == "}" {
		d.Value = ""
	} else if b, ok := strings.CutSuffix(body, "\n}"); ok {
		d.Value = b
	}
	return d
}

// Load reads and parses a dump file.
func Load(file string, opt Options) (*Set, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return Parse(data, opt)
}

// zshDefaults are aliases zsh or distro zshrc files define on their own.
var zshDefaults = map[string]bool{"run-help": true, "which-command": true}

func skipReason(d Def, names []string, sources []glob, cache map[string]string) string {
	if d.Kind != Function && zshDefaults[d.Name] {
		return "zsh default"
	}
	for _, g := range names {
		if ok, _ := path.Match(g, d.Name); ok {
			return "matches ignore " + g
		}
	}
	if d.Kind != Function {
		return ""
	}
	n := d.Name
	switch {
	case n == "":
		return "empty name"
	case strings.HasPrefix(n, "_"), strings.HasPrefix(n, "."),
		strings.HasPrefix(n, "+"), strings.HasPrefix(n, "-"):
		return "internal name"
	case strings.HasPrefix(n, "prompt_"):
		return "prompt theme"
	case n == "shtick":
		return "shtick itself"
	case hookNames[n], strings.HasPrefix(n, "TRAP"):
		return "hook"
	case strings.Contains(d.Value, "builtin autoload -X"):
		return "autoload stub, not loaded yet"
	}
	if d.Source == "" {
		return ""
	}
	if reason, ok := cache[d.Source]; ok {
		return reason
	}
	reason := ""
	for _, g := range sources {
		if g.match(d.Source) {
			reason = "framework source " + g.pattern
			break
		}
	}
	cache[d.Source] = reason
	return reason
}

// glob is a pattern where * also crosses slashes, which is what people expect
// from patterns like */.oh-my-zsh/*. Patterns are split once up front because
// the hook checks every function on every command.
type glob struct {
	pattern string
	parts   []string
}

func compileGlob(p string) glob { return glob{pattern: p, parts: strings.Split(p, "*")} }

func (g glob) match(s string) bool {
	parts := g.parts
	if len(parts) == 1 {
		return g.pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(s, parts[i])
		if idx < 0 {
			return false
		}
		s = s[idx+len(parts[i]):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

func globMatch(pattern, s string) bool { return compileGlob(pattern).match(s) }
