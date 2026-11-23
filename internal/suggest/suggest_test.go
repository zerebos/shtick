package suggest

import (
	"testing"

	"github.com/zerebos/shtick/internal/defs"
)

func TestFind(t *testing.T) {
	s := &defs.Set{
		Aliases:   map[string]defs.Def{"gst": {Kind: defs.Alias, Name: "gst", Value: "git status"}},
		Globals:   map[string]defs.Def{},
		Functions: map[string]defs.Def{},
	}
	var lines []string
	for i := 0; i < 12; i++ {
		lines = append(lines,
			"docker compose up -d",
			"git status", // covered by gst
			"kubectl get pods",
			"cat ./file.txt", // path, not plain
		)
	}
	lines = append(lines, "rare command here")
	got := Find(lines, s, Options{MinCount: 10, Taken: func(n string) bool { return n == "kgp" }})
	byCmd := map[string]Suggestion{}
	for _, g := range got {
		byCmd[g.Command] = g
	}
	if _, ok := byCmd["git status"]; ok {
		t.Error("git status is covered by gst")
	}
	if _, ok := byCmd["docker compose"]; ok {
		t.Error("docker compose should be shadowed by docker compose up")
	}
	if g, ok := byCmd["docker compose up"]; !ok || g.Name != "dcu" || g.Count != 12 {
		t.Errorf("docker compose up = %+v", g)
	}
	if g := byCmd["kubectl get pods"]; g.Name != "kgpo" {
		t.Errorf("kubectl get pods name = %q, want kgpo since kgp is taken", g.Name)
	}
	if _, ok := byCmd["rare command"]; ok {
		t.Error("rare command under threshold")
	}
}

func TestName(t *testing.T) {
	taken := map[string]bool{"gs": true, "gst": true}
	if got := name("git stash", func(s string) bool { return taken[s] }); got != "gsta" {
		t.Errorf("got %q", got)
	}
	if got := name("npm run --silent", func(string) bool { return false }); got != "nrs" {
		t.Errorf("got %q", got)
	}
}
