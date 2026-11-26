package history

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ParsePSReadLine splits a PSReadLine history file into entries. PSReadLine
// stores a multiline command as lines ending in a backtick, and keeps no
// timestamps, so every entry's Time is zero.
func ParsePSReadLine(data []byte) []Entry {
	text := strings.TrimPrefix(string(data), "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []Entry
	var cur strings.Builder
	add := func() {
		if s := cur.String(); strings.TrimSpace(s) != "" {
			out = append(out, Entry{Line: s})
		}
		cur.Reset()
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(line, "`") {
			cur.WriteString(line[:len(line)-1])
			cur.WriteByte('\n')
			continue
		}
		cur.WriteString(line)
		add()
	}
	add()
	return out
}

// PSReadLinePath is PSReadLine's default history file for the console host.
// The plugin passes the real one (Get-PSReadLineOption).HistorySavePath, so
// this is only a fallback.
func PSReadLinePath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "PowerShell", "PSReadLine", "ConsoleHost_history.txt")
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "powershell", "PSReadLine", "ConsoleHost_history.txt")
}
