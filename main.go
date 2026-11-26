// Command shtick tracks which zsh, bash, and PowerShell aliases and functions
// you use, nudges you when you type the long form, and reports what you never
// touch.
package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/zerebos/shtick/internal/defs"
	"github.com/zerebos/shtick/internal/history"
	"github.com/zerebos/shtick/internal/match"
	"github.com/zerebos/shtick/internal/report"
	"github.com/zerebos/shtick/internal/store"
	"github.com/zerebos/shtick/internal/suggest"
)

//go:embed shell/shtick.plugin.zsh
var zshPlugin string

//go:embed shell/shtick.bash
var bashPlugin string

//go:embed shell/shtick.plugin.ps1
var pwshPlugin string

var version = "0.1.0-dev"

const usage = `shtick tracks your zsh, bash, and PowerShell aliases and functions.

usage:
  shtick report            top used, never used, and missed
  shtick top [-n N]        most used aliases and functions
  shtick unused            defined but never used
  shtick missed [-n N]     times you typed the long form
  shtick suggest           frequent commands with no alias or function
  shtick import [--force]  backfill usage from your shell history
  shtick defs [--skipped]  what shtick tracks, and what it skips
  shtick init zsh|bash     print the shell plugin, for eval "$(shtick init zsh)"
  shtick init pwsh         print the PowerShell plugin, for
                           Invoke-Expression (& shtick init pwsh | Out-String)
  shtick version

Run reports through the plugin's shtick function so they see this shell's
aliases and functions. Data stays in ~/.local/share/shtick.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now()))
}

type env struct {
	out, err io.Writer
	now      time.Time
	paths    store.Paths
}

func run(args []string, stdout, stderr io.Writer, now time.Time) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	e := env{out: stdout, err: stderr, now: now, paths: store.DefaultPaths()}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "hook":
		e.hook(rest)
		return 0 // never fail the shell
	case "report":
		err = e.report(rest)
	case "top":
		err = e.top(rest)
	case "unused":
		err = e.unused(rest)
	case "missed":
		err = e.missed(rest)
	case "suggest":
		err = e.suggest(rest)
	case "import":
		err = e.importHistory(rest)
	case "defs":
		err = e.defs(rest)
	case "init":
		err = e.init(rest)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "shtick", version)
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "shtick: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "shtick:", err)
		return 1
	}
	return 0
}

func (e env) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("shtick "+name, flag.ContinueOnError)
	fs.SetOutput(e.err)
	return fs
}

func defsOptions() defs.Options {
	var opt defs.Options
	if v := os.Getenv("SHTICK_IGNORE"); v != "" {
		opt.IgnoreNames = strings.Fields(v)
	}
	if v, ok := os.LookupEnv("SHTICK_IGNORE_SOURCES"); ok {
		opt.IgnoreSources = strings.Fields(v)
		if opt.IgnoreSources == nil {
			opt.IgnoreSources = []string{} // set but empty: skip nothing by source
		}
	}
	return opt
}

// loadDefs finds this shell's dump: --defs, then SHTICK_DEFS, then the newest.
func (e env) loadDefs(flagValue string) (*defs.Set, error) {
	file := flagValue
	if file == "" {
		file = os.Getenv("SHTICK_DEFS")
	}
	if file == "" {
		latest, err := e.paths.LatestDefs()
		if err != nil {
			return nil, errors.New("no alias and function list found; load the shell plugin (eval \"$(shtick init zsh)\" or \"$(shtick init bash)\", or shtick init pwsh) and open a new prompt")
		}
		file = latest
	}
	set, err := defs.Load(file, defsOptions())
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	return set, nil
}

// shell is the shell whose history shtick reads: SHTICK_SHELL, which the
// plugins set, else bash when the login shell ($SHELL) is bash, else zsh.
func shell() history.Format {
	if v := os.Getenv("SHTICK_SHELL"); v != "" {
		return history.Format(v)
	}
	if filepath.Base(os.Getenv("SHELL")) == "bash" {
		return history.Bash
	}
	return history.Zsh
}

// formatFor is the history format for a definition set's shell. A PowerShell
// dump says so itself; zsh and bash dumps look alike, so those fall back to
// SHTICK_SHELL and the file name.
func formatFor(set *defs.Set) history.Format {
	if set.Shell == defs.Pwsh {
		return history.Pwsh
	}
	return shell()
}

func histfile(flagValue string, format history.Format) string {
	candidates := []string{flagValue, os.Getenv("SHTICK_HISTFILE")}
	if format != history.Pwsh {
		candidates = append(candidates, os.Getenv("HISTFILE")) // a zsh and bash variable
	}
	for _, v := range candidates {
		if v != "" {
			return v
		}
	}
	if format == history.Pwsh {
		return history.PSReadLinePath()
	}
	home, _ := os.UserHomeDir()
	if format == history.Bash {
		return filepath.Join(home, ".bash_history")
	}
	return filepath.Join(home, ".zsh_history")
}

// historyFormat picks how to parse a history file: bash for a file named like
// .bash_history, zsh for one like .zsh_history, otherwise the given format.
func historyFormat(file string, format history.Format) history.Format {
	if format == history.Pwsh {
		return format
	}
	if strings.Contains(filepath.Base(file), "bash") {
		return history.Bash
	}
	if strings.Contains(filepath.Base(file), "zsh") {
		return history.Zsh
	}
	return format
}

func toRecords(events []match.Event, ts int64, src store.Source) []store.Record {
	recs := make([]store.Record, 0, len(events))
	for _, ev := range events {
		kind := string(ev.Def)
		if ev.Kind == match.Missed {
			kind = "missed"
		}
		recs = append(recs, store.Record{Time: ts, Kind: kind, Name: ev.Name, Source: src})
	}
	return recs
}

// historyNumber is the "%5d" history number and the "*" (modified) or space
// marker that bash's `history` prints before each entry.
var historyNumber = regexp.MustCompile(`^ *[0-9]+[ *] `)

// readHistoryEntry reads `history 1` output saved by the bash plugin and
// returns the command line without its number. HISTTIMEFORMAT is cleared
// before it is written, so there is no timestamp.
func readHistoryEntry(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	s := strings.TrimSuffix(string(data), "\n")
	loc := historyNumber.FindStringIndex(s)
	if loc == nil {
		return "", fmt.Errorf("%s: not a history entry", file)
	}
	return s[loc[1]:], nil
}

// hook records one typed line. The line comes from the arguments, from
// --history-entry (bash), or from SHTICK_LINE when there are neither:
// PowerShell passes it that way because Windows PowerShell 5.1 mangles quotes
// in native command arguments. --stdout prints nudges to stdout, for a plugin
// that captures and shows them itself.
func (e env) hook(args []string) {
	fs := e.flags("hook")
	defsFile := fs.String("defs", "", "definitions dump")
	toStdout := fs.Bool("stdout", false, "print nudges to stdout")
	entryFile := fs.String("history-entry", "", "file holding bash `history 1` output, instead of the line as arguments")
	if fs.Parse(args) != nil {
		return
	}
	line := strings.Join(fs.Args(), " ")
	if *entryFile != "" {
		var err error
		if line, err = readHistoryEntry(*entryFile); err != nil {
			e.debug(err)
			return
		}
	} else if len(fs.Args()) == 0 {
		line = os.Getenv("SHTICK_LINE")
	}
	if strings.TrimSpace(line) == "" {
		return
	}
	set, err := e.loadDefs(*defsFile)
	if err != nil {
		e.debug(err)
		return
	}
	events := match.New(set).Line(line)
	if os.Getenv("SHTICK_NUDGE") != "0" {
		w := e.err
		if *toStdout {
			w = e.out
		}
		for _, ev := range events {
			if ev.Kind == match.Missed {
				fmt.Fprintf(w, "shtick: %s = %s\n", ev.Name, ev.Expansion)
			}
		}
	}
	if err := e.paths.Append(toRecords(events, e.now.Unix(), store.FromHook)); err != nil {
		e.debug(err)
	}
}

func (e env) debug(err error) {
	if os.Getenv("SHTICK_DEBUG") != "" {
		fmt.Fprintln(e.err, "shtick:", err)
	}
}

func (e env) rows(defsFile string) ([]report.Row, error) {
	e.paths.PruneDefs()
	set, err := e.loadDefs(defsFile)
	if err != nil {
		return nil, err
	}
	recs, err := e.paths.Read()
	if err != nil {
		return nil, err
	}
	return report.Build(set, recs), nil
}

func (e env) printer() report.Printer { return report.Printer{W: e.out, Now: e.now} }

func (e env) report(args []string) error {
	fs := e.flags("report")
	defsFile := fs.String("defs", "", "definitions dump")
	n := fs.Int("n", 10, "rows in the top and missed sections")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rows, err := e.rows(*defsFile)
	if err != nil {
		return err
	}
	p := e.printer()
	fmt.Fprintln(e.out, "Most used")
	p.Top(report.Top(rows, *n))
	fmt.Fprintln(e.out, "\nNever used")
	p.Unused(report.Unused(rows))
	fmt.Fprintln(e.out, "\nTyped the long way")
	p.Missed(report.MissedRows(rows, *n))
	return nil
}

func (e env) top(args []string) error {
	fs := e.flags("top")
	defsFile := fs.String("defs", "", "definitions dump")
	n := fs.Int("n", 20, "rows to show, 0 for all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rows, err := e.rows(*defsFile)
	if err != nil {
		return err
	}
	e.printer().Top(report.Top(rows, *n))
	return nil
}

func (e env) unused(args []string) error {
	fs := e.flags("unused")
	defsFile := fs.String("defs", "", "definitions dump")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rows, err := e.rows(*defsFile)
	if err != nil {
		return err
	}
	e.printer().Unused(report.Unused(rows))
	return nil
}

func (e env) missed(args []string) error {
	fs := e.flags("missed")
	defsFile := fs.String("defs", "", "definitions dump")
	n := fs.Int("n", 20, "rows to show, 0 for all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rows, err := e.rows(*defsFile)
	if err != nil {
		return err
	}
	e.printer().Missed(report.MissedRows(rows, *n))
	return nil
}

func (e env) defs(args []string) error {
	fs := e.flags("defs")
	defsFile := fs.String("defs", "", "definitions dump")
	skipped := fs.Bool("skipped", false, "also list skipped definitions and why")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := e.loadDefs(*defsFile)
	if err != nil {
		return err
	}
	e.printer().Defs(set, *skipped)
	return nil
}

func (e env) suggest(args []string) error {
	fs := e.flags("suggest")
	defsFile := fs.String("defs", "", "definitions dump")
	hist := fs.String("histfile", "", "history file (default $HISTFILE, or ~/.zsh_history or ~/.bash_history; PSReadLine's for PowerShell)")
	minCount := fs.Int("min", 10, "minimum times typed")
	n := fs.Int("n", 10, "suggestions to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := e.loadDefs(*defsFile)
	if err != nil {
		return err
	}
	format := formatFor(set)
	file := histfile(*hist, format)
	entries, err := history.Load(file, historyFormat(file, format))
	if err != nil {
		return err
	}
	lines := make([]string, len(entries))
	for i, en := range entries {
		lines[i] = en.Line
	}
	words := reserved
	if set.Shell == defs.Pwsh {
		words = pwshReserved
	}
	taken := func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil || words[strings.ToLower(name)]
	}
	sugs := suggest.Find(lines, set, suggest.Options{MinCount: *minCount, Limit: *n, Taken: taken})
	if len(sugs) == 0 {
		fmt.Fprintln(e.out, "nothing typed often enough to suggest")
		return nil
	}
	for _, s := range sugs {
		if set.Shell == defs.Pwsh {
			// PowerShell aliases cannot carry arguments, so suggest a function
			fmt.Fprintf(e.out, "function %s { %s @args }  # typed %d times\n", s.Name, s.Command, s.Count)
			continue
		}
		fmt.Fprintf(e.out, "alias %s='%s'  # typed %d times\n", s.Name, s.Command, s.Count)
	}
	return nil
}

// zsh and bash builtins and reserved words a suggested name must not shadow.
var reserved = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`alias autoload bg bindkey break builtin bye cd chdir command
		compadd continue declare dirs disable disown echo emulate enable eval exec exit export
		false fc fg float functions getopts hash history integer jobs kill let limit local log
		logout noglob popd print printf pushd pwd r read readonly rehash return sched set setopt
		shift source suspend test times trap true ttyctl type typeset ulimit umask unalias
		unfunction unhash unlimit unset unsetopt vared wait whence where which zcompile zle
		zmodload zparseopts zstyle do done if fi then else elif case esac for while until
		select time repeat function bind caller compgen complete compopt coproc help
		mapfile readarray shopt`) {
		reserved[w] = true
	}
	for _, w := range strings.Fields(`begin break catch class clean configuration continue data define
		do dynamicparam else elseif end enum exit filter finally for foreach from function hidden
		if in inlinescript parallel param process return sequence static switch throw trap try
		until using var while workflow`) {
		pwshReserved[w] = true
	}
}

// PowerShell keywords a suggested name must not shadow. Built-in aliases are
// in the dump and checked there.
var pwshReserved = map[string]bool{}

func (e env) importHistory(args []string) error {
	fs := e.flags("import")
	defsFile := fs.String("defs", "", "definitions dump")
	hist := fs.String("histfile", "", "history file (default $HISTFILE, or ~/.zsh_history or ~/.bash_history; PSReadLine's for PowerShell)")
	force := fs.Bool("force", false, "replace an earlier import")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := e.loadDefs(*defsFile)
	if err != nil {
		return err
	}
	format := formatFor(set)
	file := histfile(*hist, format)
	format = historyFormat(file, format)
	var entries []history.Entry
	noTimes := false // history without timestamps, cut at the plugin's mark instead
	if format == history.Pwsh {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if mark, ok := e.paths.HistoryMark(string(format)); ok && mark < int64(len(data)) {
			data = data[:mark]
		}
		entries = history.ParsePSReadLine(data)
		noTimes = true
	} else if entries, err = history.Load(file, format); err != nil {
		return err
	}
	existing, err := e.paths.Read()
	if err != nil {
		return err
	}

	src := store.HistorySource(string(format))
	var firstHook int64
	imported := false
	kept := existing[:0:0]
	for _, r := range existing {
		if r.Source == src {
			imported = true
			continue
		}
		kept = append(kept, r)
		if r.Source == store.FromHook && (firstHook == 0 || r.Time < firstHook) {
			firstHook = r.Time
		}
	}
	if imported && !*force {
		return errors.New("history was already imported; use --force to redo it")
	}

	m := match.New(set)
	var recs []store.Record
	skipped := 0
	for _, en := range entries {
		// entries at or after the first hook event were already recorded live
		if !noTimes && firstHook != 0 && (en.Time == 0 || en.Time >= firstHook) {
			skipped++
			continue
		}
		recs = append(recs, toRecords(m.Line(en.Line), en.Time, src)...)
	}

	if imported {
		if err := e.paths.Rewrite(append(kept, recs...)); err != nil {
			return err
		}
	} else if err := e.paths.Append(recs); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "imported %d events from %d history entries in %s\n", len(recs), len(entries)-skipped, file)
	if skipped > 0 {
		fmt.Fprintf(e.out, "skipped %d entries already covered by live tracking or without timestamps\n", skipped)
	}
	return nil
}

func (e env) init(args []string) error {
	if len(args) == 1 {
		switch args[0] {
		case "zsh":
			fmt.Fprint(e.out, zshPlugin)
			return nil
		case "bash":
			fmt.Fprint(e.out, bashPlugin)
			return nil
		case "pwsh", "powershell":
			fmt.Fprint(e.out, pwshPlugin)
			return nil
		}
	}
	return errors.New("usage: shtick init zsh|bash|pwsh")
}
