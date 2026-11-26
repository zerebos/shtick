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

func TestParseBash(t *testing.T) {
	data := dump(
		[4]string{"shell", "bash", "", ""},
		[4]string{"alias", "gst", "", "git status"},
		[4]string{"function", "gco", "gco 3 /home/z/.bashrc\n", "gco () \n{ \n    git checkout \"$@\"\n}\n"},
		[4]string{"function", "typed", "typed 1 main\n", "typed () \n{ \n    git fetch;\n    git status\n}\n"},
		[4]string{"function", "empty", "empty 2 environment\n", "empty () \n{ \n}\n"},
		[4]string{"function", "redir", "redir 9 /home/my dir/fns.sh\n", "redir () \n{ \n    echo hi\n} > /dev/null\n"},
		[4]string{"function", "_git", "_git 1 /usr/share/bash-completion/completions/git\n", "_git () \n{ \n    :\n}\n"},
		[4]string{"function", "gitit", "gitit 1 /home/z/.bash_it/lib/x.bash\n", "gitit () \n{ \n    :\n}\n"},
		[4]string{"function", "command_not_found_handle", "command_not_found_handle 1 /etc/bash.bashrc\n", "command_not_found_handle () \n{ \n    :\n}\n"},
	)
	s, err := Parse(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Shell != Bash || s.Aliases["gst"].Shell != Bash {
		t.Errorf("shell = %q, alias shell = %q, want bash", s.Shell, s.Aliases["gst"].Shell)
	}
	want := map[string][2]string{
		"gco":   {"/home/z/.bashrc", "    git checkout \"$@\""},
		"typed": {"", "    git fetch;\n    git status"},
		"empty": {"", ""},
		"redir": {"/home/my dir/fns.sh", "redir () \n{ \n    echo hi\n} > /dev/null\n"},
	}
	if len(s.Functions) != len(want) {
		t.Errorf("functions = %v", s.Functions)
	}
	for name, w := range want {
		d := s.Functions[name]
		if d.Source != w[0] || d.Value != w[1] {
			t.Errorf("%s: source %q value %q, want %q %q", name, d.Source, d.Value, w[0], w[1])
		}
	}
	reasons := map[string]string{}
	for _, sk := range s.Skipped {
		reasons[sk.Name] = sk.Reason
	}
	for name, want := range map[string]string{"_git": "internal", "gitit": "framework", "command_not_found_handle": "hook"} {
		if !strings.Contains(reasons[name], want) {
			t.Errorf("%s skipped for %q, want %q", name, reasons[name], want)
		}
	}
	if _, ok := reasons["bash"]; ok || len(s.Skipped) != 3 {
		t.Errorf("the shell record is not a definition, skipped = %v", s.Skipped)
	}
}

// TestParseZshLeavesBashShapes checks that a dump without a shell record is
// zsh, and that a zsh function whose body happens to look like `declare -f`
// output is not unwrapped.
func TestParseZshLeavesBashShapes(t *testing.T) {
	body := "odd () \n{ \n    echo hi\n}\n"
	src := "odd 1 /home/z/.zshrc\n"
	s, err := Parse(dump([4]string{"function", "odd", src, body}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Shell != Zsh {
		t.Errorf("shell = %q, want zsh", s.Shell)
	}
	if d := s.Functions["odd"]; d.Value != body || d.Source != src || d.Shell != Zsh {
		t.Errorf("zsh function changed: %+v", d)
	}
}
