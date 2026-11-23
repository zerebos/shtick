// Package report turns the usage log into tables.
package report

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/zerebos/shtick/internal/defs"
	"github.com/zerebos/shtick/internal/match"
	"github.com/zerebos/shtick/internal/store"
)

// Row is one definition with its counts.
type Row struct {
	Def        defs.Def
	Uses       int
	LastUsed   int64
	Missed     int
	LastMissed int64
}

// Build joins the log with the current definitions. Names that no longer exist
// are dropped.
func Build(set *defs.Set, recs []store.Record) []Row {
	rows := map[string]*Row{}
	key := func(k defs.Kind, n string) string { return string(k) + "\x00" + n }
	for _, d := range set.All() {
		rows[key(d.Kind, d.Name)] = &Row{Def: d}
	}
	for _, r := range recs {
		if r.Kind == "missed" {
			// missed records name an alias or function; try both
			for _, k := range []defs.Kind{defs.Alias, defs.Function} {
				if row, ok := rows[key(k, r.Name)]; ok {
					row.Missed++
					row.LastMissed = max(row.LastMissed, r.Time)
					break
				}
			}
			continue
		}
		if row, ok := rows[key(defs.Kind(r.Kind), r.Name)]; ok {
			row.Uses++
			row.LastUsed = max(row.LastUsed, r.Time)
		}
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Def.Name < out[j].Def.Name })
	return out
}

// Top returns used rows, most used first.
func Top(rows []Row, n int) []Row {
	var out []Row
	for _, r := range rows {
		if r.Uses > 0 {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Uses != out[j].Uses {
			return out[i].Uses > out[j].Uses
		}
		return out[i].LastUsed > out[j].LastUsed
	})
	return limit(out, n)
}

// Unused returns rows never used, aliases first.
func Unused(rows []Row) []Row {
	var out []Row
	for _, r := range rows {
		if r.Uses == 0 {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Def.Kind != out[j].Def.Kind {
			return out[i].Def.Kind < out[j].Def.Kind
		}
		return out[i].Def.Name < out[j].Def.Name
	})
	return out
}

// MissedRows returns rows you typed the long form of, most missed first.
func MissedRows(rows []Row, n int) []Row {
	var out []Row
	for _, r := range rows {
		if r.Missed > 0 {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Missed != out[j].Missed {
			return out[i].Missed > out[j].Missed
		}
		return out[i].Def.Name < out[j].Def.Name
	})
	return limit(out, n)
}

func limit(rows []Row, n int) []Row {
	if n > 0 && len(rows) > n {
		return rows[:n]
	}
	return rows
}

// Printer writes tables.
type Printer struct {
	W   io.Writer
	Now time.Time
}

func (p Printer) tab() *tabwriter.Writer {
	return tabwriter.NewWriter(p.W, 0, 0, 2, ' ', 0)
}

// When formats a timestamp as a short relative age.
func (p Printer) When(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	d := p.Now.Sub(time.Unix(ts, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return time.Unix(ts, 0).Format("2006-01-02")
	}
}

// Top prints the most used table.
func (p Printer) Top(rows []Row) {
	if len(rows) == 0 {
		fmt.Fprintln(p.W, "nothing recorded yet")
		return
	}
	w := p.tab()
	fmt.Fprintln(w, "NAME\tKIND\tUSES\tLAST USED")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", r.Def.Name, r.Def.Kind, r.Uses, p.When(r.LastUsed))
	}
	w.Flush()
}

// Unused prints the never used table.
func (p Printer) Unused(rows []Row) {
	if len(rows) == 0 {
		fmt.Fprintln(p.W, "everything has been used at least once")
		return
	}
	w := p.tab()
	fmt.Fprintln(w, "NAME\tKIND\tDEFINITION")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Def.Name, r.Def.Kind, Summary(r.Def, 48))
	}
	w.Flush()
}

// Missed prints the long form table.
func (p Printer) Missed(rows []Row) {
	if len(rows) == 0 {
		fmt.Fprintln(p.W, "no missed shortcuts")
		return
	}
	w := p.tab()
	fmt.Fprintln(w, "NAME\tKIND\tMISSED\tUSES\tYOU TYPED")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n", r.Def.Name, r.Def.Kind, r.Missed, r.Uses, Summary(r.Def, 48))
	}
	w.Flush()
}

// Defs prints what is tracked, and optionally what is skipped.
func (p Printer) Defs(set *defs.Set, skipped bool) {
	w := p.tab()
	fmt.Fprintln(w, "NAME\tKIND\tNUDGE\tSOURCE")
	for _, d := range set.All() {
		nudge := "yes"
		if d.Kind == defs.Function && !match.Nudgeable(d) {
			nudge = "no"
		}
		if d.Kind == defs.Global {
			nudge = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.Name, d.Kind, nudge, tilde(d.Source))
	}
	w.Flush()
	if !skipped {
		fmt.Fprintf(p.W, "\n%d skipped (use --skipped to list)\n", len(set.Skipped))
		return
	}
	fmt.Fprintln(p.W)
	w = p.tab()
	fmt.Fprintln(w, "SKIPPED\tKIND\tREASON")
	for _, s := range set.Skipped {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.Kind, s.Reason)
	}
	w.Flush()
}

// Summary is a one line view of a definition.
func Summary(d defs.Def, width int) string {
	v := strings.Join(strings.Fields(d.Value), " ")
	if d.Kind == defs.Function {
		v = "{ " + v + " }"
	}
	if r := []rune(v); len(r) > width {
		v = string(r[:width-1]) + "…"
	}
	return v
}

func tilde(p string) string {
	if p == "" {
		return "-"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}
