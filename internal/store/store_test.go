package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestAppendReadRewrite(t *testing.T) {
	p := Paths{Data: t.TempDir(), State: t.TempDir()}
	recs, err := p.Read()
	if err != nil || recs != nil {
		t.Fatalf("empty log: %v %v", recs, err)
	}
	a := []Record{{1, "alias", "gst", FromHook}, {2, "missed", "bad\tname", FromHistory}}
	if err := p.Append(a); err != nil {
		t.Fatal(err)
	}
	if err := p.Append([]Record{{3, "function", "gco", FromHook}}); err != nil {
		t.Fatal(err)
	}
	got, err := p.Read()
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{{1, "alias", "gst", FromHook}, {2, "missed", "bad name", FromHistory}, {3, "function", "gco", FromHook}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if err := p.Rewrite(want[:1]); err != nil {
		t.Fatal(err)
	}
	got, _ = p.Read()
	if len(got) != 1 {
		t.Errorf("rewrite left %v", got)
	}
	info, _ := os.Stat(p.LogFile())
	if info.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v", info.Mode().Perm())
	}
}

func TestDefsFiles(t *testing.T) {
	p := Paths{Data: t.TempDir(), State: t.TempDir()}
	os.MkdirAll(p.DefsDir(), 0o700)
	self := filepath.Join(p.DefsDir(), strconv.Itoa(os.Getpid())+".defs")
	dead := filepath.Join(p.DefsDir(), "999999999.defs")
	os.WriteFile(dead, nil, 0o600)
	os.WriteFile(dead+".line", nil, 0o600)
	os.WriteFile(self, nil, 0o600)
	os.WriteFile(self+".line", nil, 0o600)
	if n := p.PruneDefs(); n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
	if _, err := os.Stat(dead + ".line"); err == nil {
		t.Error("dead shell's scratch file left behind")
	}
	if _, err := os.Stat(self + ".line"); err != nil {
		t.Error("live shell's scratch file removed")
	}
	got, err := p.LatestDefs()
	if err != nil || got != self {
		t.Errorf("latest = %q %v", got, err)
	}
}

func TestDefaultPathsEnv(t *testing.T) {
	t.Setenv("SHTICK_DATA_DIR", "/x/data")
	t.Setenv("SHTICK_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/x/state")
	p := DefaultPaths()
	if p.Data != "/x/data" || p.State != "/x/state/shtick" {
		t.Errorf("got %+v", p)
	}
}
