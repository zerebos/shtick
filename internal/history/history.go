// Package history reads zsh history files, plain or extended.
package history

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Entry is one history entry. Time is zero when the file has no timestamps.
type Entry struct {
	Time int64
	Line string
}

// meta is zsh's metafication marker: a byte after it was XORed with 32.
const meta = 0x83

// Unmetafy reverses zsh's history encoding of special bytes.
func Unmetafy(b []byte) []byte {
	if bytes.IndexByte(b, meta) < 0 {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == meta && i+1 < len(b) {
			i++
			out = append(out, b[i]^32)
			continue
		}
		out = append(out, b[i])
	}
	return out
}

var extended = regexp.MustCompile(`^: (\d+):\d+;`)

// Parse splits history data into entries. Lines ending in an odd number of
// backslashes continue onto the next line, which is how zsh stores multiline
// commands.
func Parse(data []byte) []Entry {
	text := string(Unmetafy(data))
	var out []Entry
	var cur *Entry
	continues := false
	for _, line := range strings.Split(text, "\n") {
		if continues && cur != nil {
			cur.Line = cur.Line[:len(cur.Line)-1] + "\n" + line
		} else {
			e := Entry{Line: line}
			if m := extended.FindStringSubmatch(line); m != nil {
				e.Time, _ = strconv.ParseInt(m[1], 10, 64)
				e.Line = line[len(m[0]):]
			}
			out = append(out, e)
			cur = &out[len(out)-1]
		}
		continues = trailingBackslashes(line)%2 == 1
	}
	// drop blank entries, including the one after a final newline
	kept := out[:0]
	for _, e := range out {
		if strings.TrimSpace(e.Line) != "" {
			kept = append(kept, e)
		}
	}
	return kept
}

func trailingBackslashes(s string) int {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n
}

// Load reads and parses a history file.
func Load(file string) ([]Entry, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return Parse(data), nil
}
