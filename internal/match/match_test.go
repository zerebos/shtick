package match

import (
	"reflect"
	"testing"

	"github.com/zerebos/shtick/internal/defs"
)

func set(t *testing.T) *defs.Set {
	t.Helper()
	s := &defs.Set{
		Aliases: map[string]defs.Def{
			"gst":  {Kind: defs.Alias, Name: "gst", Value: "git status"},
			"gss":  {Kind: defs.Alias, Name: "gss", Value: "git status -s"},
			"ll":   {Kind: defs.Alias, Name: "ll", Value: "ls -lah"},
			"ls":   {Kind: defs.Alias, Name: "ls", Value: "ls --color=auto"},
			"long": {Kind: defs.Alias, Name: "long", Value: "ls"}, // longer than value, never nudged
			"up":   {Kind: defs.Alias, Name: "up", Value: "cd .. && ls"},
		},
		Globals: map[string]defs.Def{
			"G": {Kind: defs.Global, Name: "G", Value: "| grep"},
		},
		Functions: map[string]defs.Def{
			"gco":   {Kind: defs.Function, Name: "gco", Value: "\tgit checkout \"$@\""},
			"dps":   {Kind: defs.Function, Name: "dps", Value: "\tdocker ps --format 'table {{.Names}}'"},
			"src":   {Kind: defs.Function, Name: "src", Value: "\tcd ~/src\n\tgit pull"},
			"mkcd":  {Kind: defs.Function, Name: "mkcd", Value: "\tmkdir -p \"$1\" && cd \"$1\""},
			"guard": {Kind: defs.Function, Name: "guard", Value: "\tif true\n\tthen\n\t\tls\n\tfi"},
		},
	}
	return s
}

func line(t *testing.T, l string) []Event {
	t.Helper()
	return New(set(t)).Line(l)
}

func TestUsed(t *testing.T) {
	got := line(t, "gst && gco main | ll G foo")
	want := []Event{
		{Kind: Used, Def: defs.Alias, Name: "gst"},
		{Kind: Used, Def: defs.Function, Name: "gco"},
		{Kind: Used, Def: defs.Global, Name: "G"},
		{Kind: Used, Def: defs.Alias, Name: "ll"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestQuotedCommandWordIsNotAliasUse(t *testing.T) {
	if got := line(t, `\gst`); len(got) != 0 {
		t.Errorf("escaped alias counted: %+v", got)
	}
	if got := line(t, `'gst'`); len(got) != 0 {
		t.Errorf("quoted alias counted: %+v", got)
	}
}

func TestMissedAliasPrefersLongest(t *testing.T) {
	got := line(t, "git status -s")
	want := []Event{{Kind: Missed, Def: defs.Alias, Name: "gss", Expansion: "git status -s"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	got = line(t, "git status --short")
	if len(got) != 1 || got[0].Name != "gst" {
		t.Errorf("git status --short: got %+v, want gst", got)
	}
}

func TestMissedAliasNotWhenAliasUsed(t *testing.T) {
	// typing ls runs the ls alias; typing its value should not nudge to itself
	got := line(t, "ls --color=auto /tmp")
	want := []Event{{Kind: Used, Def: defs.Alias, Name: "ls"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMissedMultiCommandAlias(t *testing.T) {
	got := line(t, "cd .. && ls")
	// ls is an alias itself, so the span is blocked; the used ls is reported
	if len(got) != 1 || got[0].Name != "ls" || got[0].Kind != Used {
		t.Errorf("got %+v", got)
	}
}

func TestMissedFunctionWithArgs(t *testing.T) {
	got := line(t, "git checkout -b feature")
	want := []Event{{Kind: Missed, Def: defs.Function, Name: "gco", Expansion: "git checkout"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMissedFunctionExact(t *testing.T) {
	got := line(t, "docker ps --format 'table {{.Names}}'")
	if len(got) != 1 || got[0].Name != "dps" || got[0].Kind != Missed {
		t.Errorf("got %+v, want dps missed", got)
	}
	// extra args don't match a function that takes none
	if got := line(t, "docker ps --format 'table {{.Names}}' -a"); len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestMissedMultiStatementFunction(t *testing.T) {
	got := line(t, "cd ~/src; git pull && make")
	if len(got) != 1 || got[0].Name != "src" || got[0].Expansion != "cd ~/src; git pull" {
		t.Errorf("got %+v, want src missed", got)
	}
}

func TestFunctionTemplates(t *testing.T) {
	s := set(t)
	for name, want := range map[string]bool{"gco": true, "dps": true, "src": true, "mkcd": false, "guard": false} {
		if got := Nudgeable(s.Functions[name]); got != want {
			t.Errorf("Nudgeable(%s) = %v, want %v", name, got, want)
		}
	}
}

func TestNoEvents(t *testing.T) {
	if got := line(t, "make test"); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}
