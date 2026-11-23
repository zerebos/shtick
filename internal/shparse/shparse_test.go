package shparse

import (
	"reflect"
	"testing"
)

func texts(cmds []Command) [][]string {
	var out [][]string
	for _, c := range cmds {
		out = append(out, c.Texts())
	}
	return out
}

func TestSplit(t *testing.T) {
	tests := []struct {
		line string
		want [][]string
	}{
		{"git status", [][]string{{"git", "status"}}},
		{"git add . && git commit -m 'a; b'", [][]string{{"git", "add", "."}, {"git", "commit", "-m", "a; b"}}},
		{`echo "x | y" | grep x`, [][]string{{"echo", "x | y"}, {"grep", "x"}}},
		{"ls; pwd & jobs", [][]string{{"ls"}, {"pwd"}, {"jobs"}}},
		{"echo $(git rev-parse HEAD) done", [][]string{{"echo", "$(git rev-parse HEAD)", "done"}}},
		{"diff <(ls a) <(ls b)", [][]string{{"diff", "<(ls a)", "<(ls b)"}}},
		{"(cd src && make)", [][]string{{"cd", "src"}, {"make"}}},
		{"make 2>&1 | less", [][]string{{"make", "2>&1"}, {"less"}}},
		{"cmd &> log", [][]string{{"cmd", "&>", "log"}}},
		{"git status # check", [][]string{{"git", "status"}}},
		{"echo a\\ b", [][]string{{"echo", "a b"}}},
		{"line one \\\n continued", [][]string{{"line", "one", "continued"}}},
		{"echo `date`", [][]string{{"echo", "`date`"}}},
		{"", nil},
	}
	for _, tt := range tests {
		if got := texts(Split(tt.line)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Split(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestRawKeepsQuotes(t *testing.T) {
	c := Split(`git commit -m "hi there"`)[0]
	if c[3].Raw != `"hi there"` || c[3].Text != "hi there" {
		t.Fatalf("got raw %q text %q", c[3].Raw, c[3].Text)
	}
}

func TestStrip(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{"FOO=1 BAR=2 make test", []string{"make", "test"}},
		{"sudo -E apt update", []string{"apt", "update"}},
		{"noglob command git status", []string{"git", "status"}},
		{"for f in *; do", nil},
		{"if gst", []string{"gst"}},
		{"arr[1]=x ls", []string{"ls"}},
		{"PATH+=:/x ls", []string{"ls"}},
	}
	for _, tt := range tests {
		got := Strip(Split(tt.line)[0]).Texts()
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Strip(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
