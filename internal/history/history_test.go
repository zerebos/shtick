package history

import (
	"reflect"
	"testing"
)

func TestParseExtended(t *testing.T) {
	data := []byte(": 1700000000:0;git status\n: 1700000010:2;for f in *; do\\\necho $f\\\ndone\n: 1700000020:0;ls\n")
	want := []Entry{
		{1700000000, "git status"},
		{1700000010, "for f in *; do\necho $f\ndone"},
		{1700000020, "ls"},
	}
	if got := Parse(data); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestParsePlain(t *testing.T) {
	data := []byte("git status\n\nls -la\necho a\\\\\nnext\n")
	want := []Entry{{0, "git status"}, {0, "ls -la"}, {0, `echo a\\`}, {0, "next"}}
	if got := Parse(data); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestUnmetafy(t *testing.T) {
	// zsh stores 0x83 0xa2 for byte 0x82, part of the UTF-8 for "é"? use a direct case
	in := []byte{'a', 0x83, 0xa3, 'b'} // 0xa3 ^ 32 = 0x83
	if got := Unmetafy(in); !reflect.DeepEqual(got, []byte{'a', 0x83, 'b'}) {
		t.Errorf("got %v", got)
	}
	// é is c3 a9; zsh metafies bytes >= 0x83, so c3 stays and a9 stays
	if got := string(Unmetafy([]byte("caf\xc3\xa9"))); got != "café" {
		t.Errorf("got %q", got)
	}
}
