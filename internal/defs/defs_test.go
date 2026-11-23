package defs

import (
	"strings"
	"testing"
)

func dump(recs ...[4]string) []byte {
	var b strings.Builder
	for _, r := range recs {
		for _, f := range r {
			b.WriteString(f)
			b.WriteByte(0)
		}
	}
	return []byte(b.String())
}

func TestParseAndFilter(t *testing.T) {
	data := dump(
		[4]string{"alias", "gst", "", "git status"},
		[4]string{"global", "G", "", "| grep"},
		[4]string{"function", "mkcd", "/home/z/.zshrc", "mkdir -p \"$1\" && cd \"$1\""},
		[4]string{"function", "_git", "", "..."},
		[4]string{"function", "precmd", "", "..."},
		[4]string{"function", "TRAPINT", "", "..."},
		[4]string{"function", "prompt_pure_setup", "", "..."},
		[4]string{"function", "omz_thing", "/home/z/.oh-my-zsh/lib/x.zsh", "..."},
		[4]string{"function", "lazy", "", "builtin autoload -X"},
		[4]string{"function", "scratch", "", "cd /tmp"},
	)
	s, err := Parse(data, Options{IgnoreNames: []string{"scr*"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Aliases["gst"]; !ok {
		t.Error("gst alias missing")
	}
	if _, ok := s.Globals["G"]; !ok {
		t.Error("G global missing")
	}
	if len(s.Functions) != 1 || s.Functions["mkcd"].Source != "/home/z/.zshrc" {
		t.Errorf("functions = %v, want only mkcd", s.Functions)
	}
	reasons := map[string]string{}
	for _, sk := range s.Skipped {
		reasons[sk.Name] = sk.Reason
	}
	for name, want := range map[string]string{
		"_git": "internal", "precmd": "hook", "TRAPINT": "hook", "prompt_pure_setup": "prompt",
		"omz_thing": "framework", "lazy": "autoload", "scratch": "ignore",
	} {
		if !strings.Contains(reasons[name], want) {
			t.Errorf("%s skipped for %q, want %q", name, reasons[name], want)
		}
	}
	if !s.Defined("_git") || s.Defined("nope") {
		t.Error("Defined wrong")
	}
}

func TestEmptySourceGlobsKeepFramework(t *testing.T) {
	data := dump([4]string{"function", "omz_thing", "/home/z/.oh-my-zsh/lib/x.zsh", "ls"})
	s, err := Parse(data, Options{IgnoreSources: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Functions["omz_thing"]; !ok {
		t.Error("empty IgnoreSources should keep framework functions")
	}
}

func TestParseBad(t *testing.T) {
	if _, err := Parse([]byte("alias\x00x\x00"), Options{}); err == nil {
		t.Error("want error for truncated dump")
	}
	s, err := Parse(nil, Options{})
	if err != nil || len(s.All()) != 0 {
		t.Errorf("empty dump: %v %v", s, err)
	}
}

func TestGlobMatch(t *testing.T) {
	if !globMatch("*/.oh-my-zsh/*", "/home/z/.oh-my-zsh/plugins/git/git.plugin.zsh") {
		t.Error("should match nested path")
	}
	if globMatch("/usr/share/*", "/home/usr/share/x") {
		t.Error("should anchor at start")
	}
}
