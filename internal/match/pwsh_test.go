package match

import (
	"reflect"
	"testing"

	"github.com/zerebos/shtick/internal/defs"
)

func pwshSet() *defs.Set {
	def := func(k defs.Kind, name, value string) defs.Def {
		return defs.Def{Kind: k, Name: name, Value: value, Shell: defs.Pwsh}
	}
	return &defs.Set{
		Shell: defs.Pwsh,
		Aliases: map[string]defs.Def{
			"k":  def(defs.Alias, "k", "kubectl"),
			"gh": def(defs.Alias, "gh", "Get-Help"),
		},
		Globals: map[string]defs.Def{},
		Functions: map[string]defs.Def{
			"gst":    def(defs.Function, "gst", " git status @args "),
			"gco":    def(defs.Function, "gco", "\n    git checkout $args\n"),
			"Up":     def(defs.Function, "Up", " Set-Location ..; Get-ChildItem "),
			"first":  def(defs.Function, "first", " git checkout $args[0] "),
			"params": def(defs.Function, "params", " param($Name) git checkout $Name "),
			"attr":   def(defs.Function, "attr", " [CmdletBinding()] param() git status "),
			"assign": def(defs.Function, "assign", " $b = git branch --show-current; git push origin $b "),
			"pipe":   def(defs.Function, "pipe", " $input | Sort-Object "),
			"cond":   def(defs.Function, "cond", " if ($args) { git status } "),
			"cond2":  def(defs.Function, "cond2", " if($true){ git status } "),
		},
	}
}

func TestPwshUsedIgnoresCase(t *testing.T) {
	got := New(pwshSet()).Line("K get pods; GST -s | up")
	want := []Event{
		{Kind: Used, Def: defs.Alias, Name: "k"},
		{Kind: Used, Def: defs.Function, Name: "gst"},
		{Kind: Used, Def: defs.Function, Name: "Up"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestPwshMissed(t *testing.T) {
	tests := []struct {
		line string
		want []string // names nudged, in order
	}{
		{"git status -s", []string{"gst"}},
		{"Git status", []string{"gst"}},     // command names ignore case
		{"git Status", nil},                 // arguments do not
		{"kubectl get pods", []string{"k"}}, // alias with extra args
		{"get-help about_Splatting", []string{"gh"}},
		{"git checkout -b x", []string{"gco"}},
		{"Set-Location ..; Get-ChildItem", []string{"Up"}},
		// like sudo in zsh, typed words count as typed
		{"$s = git status", nil},
		{"& git status", nil},
		{"git push origin main", nil},
	}
	m := New(pwshSet())
	for _, tt := range tests {
		var got []string
		for _, e := range m.Line(tt.line) {
			if e.Kind == Missed {
				got = append(got, e.Name)
			}
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Line(%q) missed %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestPwshNudgeable(t *testing.T) {
	s := pwshSet()
	want := map[string]bool{
		"gst": true, "gco": true, "Up": true,
		"first": false, "params": false, "attr": false, "assign": false, "pipe": false, "cond": false, "cond2": false,
	}
	for name, ok := range want {
		if got := Nudgeable(s.Functions[name]); got != ok {
			t.Errorf("Nudgeable(%s) = %v, want %v", name, got, ok)
		}
	}
}
