package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/zerebos/shtick/internal/defs"
	"github.com/zerebos/shtick/internal/store"
)

func TestBuildAndSections(t *testing.T) {
	s := &defs.Set{
		Aliases:   map[string]defs.Def{"gst": {Kind: defs.Alias, Name: "gst", Value: "git status"}, "ll": {Kind: defs.Alias, Name: "ll", Value: "ls -la"}},
		Globals:   map[string]defs.Def{},
		Functions: map[string]defs.Def{"gco": {Kind: defs.Function, Name: "gco", Value: "git checkout \"$@\""}, "old": {Kind: defs.Function, Name: "old", Value: "echo"}},
	}
	recs := []store.Record{
		{Time: 10, Kind: "alias", Name: "gst"},
		{Time: 20, Kind: "alias", Name: "gst"},
		{Time: 15, Kind: "function", Name: "gco"},
		{Time: 30, Kind: "missed", Name: "gco"},
		{Time: 31, Kind: "missed", Name: "gco"},
		{Time: 40, Kind: "alias", Name: "deleted"}, // no longer defined
	}
	rows := Build(s, recs)
	top := Top(rows, 0)
	if len(top) != 2 || top[0].Def.Name != "gst" || top[0].Uses != 2 || top[0].LastUsed != 20 {
		t.Errorf("top = %+v", top)
	}
	un := Unused(rows)
	if len(un) != 2 || un[0].Def.Name != "ll" || un[1].Def.Name != "old" {
		t.Errorf("unused = %+v", un)
	}
	mi := MissedRows(rows, 0)
	if len(mi) != 1 || mi[0].Def.Name != "gco" || mi[0].Missed != 2 {
		t.Errorf("missed = %+v", mi)
	}

	var b bytes.Buffer
	p := Printer{W: &b, Now: time.Unix(20+3*3600, 0)}
	p.Top(top)
	if !strings.Contains(b.String(), "gst   alias     2     3h ago") {
		t.Errorf("top table:\n%s", b.String())
	}
}

func TestWhen(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	p := Printer{Now: now}
	for ts, want := range map[int64]string{
		0:                                 "-",
		now.Add(-30 * time.Second).Unix(): "just now",
		now.Add(-5 * time.Minute).Unix():  "5m ago",
		now.Add(-50 * time.Hour).Unix():   "2d ago",
		now.AddDate(0, -6, 0).Unix():      "2026-05-23",
	} {
		if got := p.When(ts); got != want {
			t.Errorf("When(%d) = %q, want %q", ts, got, want)
		}
	}
}

func TestSummary(t *testing.T) {
	d := defs.Def{Kind: defs.Function, Value: "\tcd ~/src\n\tls"}
	if got := Summary(d, 40); got != "{ cd ~/src ls }" {
		t.Errorf("got %q", got)
	}
	if got := Summary(defs.Def{Kind: defs.Alias, Value: strings.Repeat("a", 60)}, 10); got != "aaaaaaaaa…" {
		t.Errorf("got %q", got)
	}
}
