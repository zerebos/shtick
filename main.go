// Command shtick tracks which zsh and PowerShell aliases and functions you
// use, nudges you when you type the long form, and reports what you never
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

//go:embed shell/shtick.plugin.ps1
var pwshPlugin string

var version = "0.1.0-dev"

const usage = `shtick tracks your zsh and PowerShell aliases and functions.

usage:
  shtick report            top used, never used, and missed
  shtick top [-n N]        most used aliases and functions
  shtick unused            defined but never used
  shtick missed [-n N]     times you typed the long form
  shtick suggest           frequent commands with no alias or function
  shtick import [--force]  backfill usage from your shell history
  shtick defs [--skipped]  what shtick tracks, and what it skips
  shtick init zsh          print the zsh plugin, for eval "$(shtick init zsh)"
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
			return nil, errors.New("no alias and function list found; load the shell plugin (see shtick init) and open a new prompt")
		}
		file = latest
	}
	set, err := defs.Load(file, defsOptions())
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	return set, nil
}

func histfile(flagValue string, shell defs.Shell) string {
	candidates := []string{flagValue, os.Getenv("SHTICK_HISTFILE")}
	if shell != defs.Pwsh {
		candidates = append(candidates, os.Getenv("HISTFILE")) // a zsh variable
	}
	for _, v := range candidates {
		if v != "" {
			return v
		}
	}
	if shell == defs.Pwsh {
		return history.PSReadLinePath()
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".zsh_history")
}

// loadHistory reads a history file in the shell's format.
func loadHistory(file string, shell defs.Shell) ([]history.Entry, error) {
	if shell == defs.Pwsh {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		return history.ParsePSReadLine(data), nil
	}
	return history.Load(file)
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

// hook records one typed line. The line comes from the arguments, or from
// SHTICK_LINE when there are none: PowerShell passes it that way because
// Windows PowerShell 5.1 mangles quotes in native command arguments.
// --stdout prints nudges to stdout, for a plugin that captures and shows
// them itself.
func (e env) hook(args []string) {
	fs := e.flags("hook")
	defsFile := fs.String("defs", "", "definitions dump")
	toStdout := fs.Bool("stdout", false, "print nudges to stdout")
	if fs.Parse(args) != nil {
		return
	}
	line := strings.Join(fs.Args(), " ")
	if len(fs.Args()) == 0 {
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
	hist := fs.String("histfile", "", "history file (default $HISTFILE or ~/.zsh_history; PSReadLine's for PowerShell)")
	minCount := fs.Int("min", 10, "minimum times typed")
	n := fs.Int("n", 10, "suggestions to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := e.loadDefs(*defsFile)
	if err != nil {
		return err
	}
	entries, err := loadHistory(histfile(*hist, set.Shell), set.Shell)
	if err != nil {
		return err
	}
	lines := make([]string, len(entries))
	for i, en := range entries {
		lines[i] = en.Line
	}
	reserved := zshReserved
	if set.Shell == defs.Pwsh {
		reserved = pwshReserved
	}
	taken := func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil || reserved[strings.ToLower(name)]
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

// zsh builtins and reserved words a suggested name must not shadow.
var zshReserved = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`alias autoload bg bindkey break builtin bye cd chdir command
		compadd continue declare dirs disable disown echo emulate enable eval exec exit export
		false fc fg float functions getopts hash history integer jobs kill let limit local log
		logout noglob popd print printf pushd pwd r read readonly rehash return sched set setopt
		shift source suspend test times trap true ttyctl type typeset ulimit umask unalias
		unfunction unhash unlimit unset unsetopt vared wait whence where which zcompile zle
		zmodload zparseopts zstyle do done if fi then else elif case esac for while until
		select time repeat function`) {
		zshReserved[w] = true
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
	hist := fs.String("histfile", "", "history file (default $HISTFILE or ~/.zsh_history; PSReadLine's for PowerShell)")
	force := fs.Bool("force", false, "replace an earlier import")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := e.loadDefs(*defsFile)
	if err != nil {
		return err
	}
	file := histfile(*hist, set.Shell)
	var entries []history.Entry
	noTimes := false // history without timestamps, cut at the plugin's mark instead
	if set.Shell == defs.Pwsh {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if mark, ok := e.paths.HistoryMark(string(set.Shell)); ok && mark < int64(len(data)) {
			data = data[:mark]
		}
		entries = history.ParsePSReadLine(data)
		noTimes = true
	} else if entries, err = history.Load(file); err != nil {
		return err
	}
	existing, err := e.paths.Read()
	if err != nil {
		return err
	}

	src := store.HistorySource(string(set.Shell))
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
		case "pwsh", "powershell":
			fmt.Fprint(e.out, pwshPlugin)
			return nil
		}
	}
	return errors.New("usage: shtick init zsh|pwsh")
}
