// Package store keeps shtick's usage log and per shell definition dumps.
// Everything is local files; nothing leaves the machine.
package store

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Source says where an event came from.
type Source string

const (
	FromHook    Source = "hook"
	FromHistory Source = "history"
)

// Record is one line of the usage log. Kind is alias, global, function, or
// missed; for missed, Name is the alias or function that could have been used.
type Record struct {
	Time   int64
	Kind   string
	Name   string
	Source Source
}

// Paths are the files shtick uses.
type Paths struct {
	Data  string // usage log lives here
	State string // defs dumps live here
}

// DefaultPaths honors SHTICK_DATA_DIR and SHTICK_STATE_DIR, then the XDG
// variables, then ~/.local/share and ~/.local/state. On Windows the fallback
// is %LOCALAPPDATA%\shtick (data) and %LOCALAPPDATA%\shtick\state. The
// PowerShell plugin repeats this logic; keep them in step.
func DefaultPaths() Paths {
	home, _ := os.UserHomeDir()
	local := ""
	if runtime.GOOS == "windows" {
		local = os.Getenv("LOCALAPPDATA")
	}
	pick := func(own, xdg, fallback, win string) string {
		if v := os.Getenv(own); v != "" {
			return v
		}
		if v := os.Getenv(xdg); v != "" {
			return filepath.Join(v, "shtick")
		}
		if local != "" {
			return filepath.Join(local, "shtick", win)
		}
		return filepath.Join(home, fallback, "shtick")
	}
	return Paths{
		Data:  pick("SHTICK_DATA_DIR", "XDG_DATA_HOME", ".local/share", ""),
		State: pick("SHTICK_STATE_DIR", "XDG_STATE_HOME", ".local/state", "state"),
	}
}

// HistorySource is the log source for imports from a shell's history. zsh
// keeps the original "history" so earlier imports still count; other shells
// get their own so importing one does not block or replace another.
func HistorySource(shell string) Source {
	if shell == "" || shell == "zsh" {
		return FromHistory
	}
	return Source("history-" + shell)
}

// HistoryMarkFile records how long a shell's history file was when its plugin
// first loaded, for history formats without timestamps. Import only reads
// the part before the mark, since live tracking covers the rest.
func (p Paths) HistoryMarkFile(shell string) string {
	return filepath.Join(p.State, shell+"-history.mark")
}

// HistoryMark returns the byte offset in the mark file, if there is one.
func (p Paths) HistoryMark(shell string) (int64, bool) {
	data, err := os.ReadFile(p.HistoryMarkFile(shell))
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(string(data), "\uFEFF")), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// LogFile is the usage log.
func (p Paths) LogFile() string { return filepath.Join(p.Data, "usage.tsv") }

// DefsDir holds one dump per shell.
func (p Paths) DefsDir() string { return filepath.Join(p.State, "defs") }

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
}

func encode(recs []Record) []byte {
	var b bytes.Buffer
	for _, r := range recs {
		fmt.Fprintf(&b, "%d\t%s\t%s\t%s\n", r.Time, clean(r.Kind), clean(r.Name), r.Source)
	}
	return b.Bytes()
}

// Append adds records to the log in a single O_APPEND write.
func (p Paths) Append(recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := os.MkdirAll(p.Data, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p.LogFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(encode(recs))
	cerr := f.Close()
	return errors.Join(werr, cerr)
}

// Read returns every record in the log. A missing log is empty. Malformed
// lines are skipped.
func (p Paths) Read() ([]Record, error) {
	f, err := os.Open(p.LogFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parse(f)
}

func parse(r io.Reader) ([]Record, error) {
	var out []Record
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), "\t")
		if len(parts) != 4 {
			continue
		}
		ts, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, Record{Time: ts, Kind: parts[1], Name: parts[2], Source: Source(parts[3])})
	}
	return out, sc.Err()
}

// Rewrite replaces the log atomically (write temp, rename).
func (p Paths) Rewrite(recs []Record) error {
	if err := os.MkdirAll(p.Data, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(p.Data, "usage-*.tsv")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(encode(recs)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p.LogFile())
}

// LatestDefs returns the newest dump in the defs directory, for running
// reports outside the plugin's wrapper.
func (p Paths) LatestDefs() (string, error) {
	entries, err := os.ReadDir(p.DefsDir())
	if err != nil {
		return "", err
	}
	var best string
	var bestT time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".defs") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestT) {
			best, bestT = filepath.Join(p.DefsDir(), e.Name()), info.ModTime()
		}
	}
	if best == "" {
		return "", os.ErrNotExist
	}
	return best, nil
}

// PruneDefs deletes dumps whose shell is gone. It returns how many it removed.
func (p Paths) PruneDefs() int {
	entries, err := os.ReadDir(p.DefsDir())
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		pidStr, ok := strings.CutSuffix(e.Name(), ".defs")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			continue
		}
		if alive(pid) {
			continue
		}
		if os.Remove(filepath.Join(p.DefsDir(), e.Name())) == nil {
			n++
		}
	}
	return n
}
