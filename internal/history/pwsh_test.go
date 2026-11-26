package history

import (
	"reflect"
	"testing"
)

func TestParsePSReadLine(t *testing.T) {
	data := "\uFEFFgit status\r\nfunction f {`\r\n  git pull`\r\n}\r\n\r\necho done\n"
	got := ParsePSReadLine([]byte(data))
	want := []Entry{
		{Line: "git status"},
		{Line: "function f {\n  git pull\n}"},
		{Line: "echo done"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
