package shparse

import (
	"reflect"
	"testing"
)

func TestSplitPwsh(t *testing.T) {
	tests := []struct {
		line string
		want [][]string
	}{
		{"git status", [][]string{{"git", "status"}}},
		{"git add . && git commit -m 'a; b'", [][]string{{"git", "add", "."}, {"git", "commit", "-m", "a; b"}}},
		{`Write-Output "x | y" | Select-String x`, [][]string{{"Write-Output", "x | y"}, {"Select-String", "x"}}},
		{"ls; pwd || echo no", [][]string{{"ls"}, {"pwd"}, {"echo", "no"}}},
		{"echo 'it''s'", [][]string{{"echo", "it's"}}},
		{"echo \"say \"\"hi\"\" `\"now`\"\"", [][]string{{"echo", `say "hi" "now"`}}},
		{"echo $(git rev-parse HEAD) done", [][]string{{"echo", "$(git rev-parse HEAD)", "done"}}},
		{`echo "head: $(git log -1 | Select-Object -First 1)"`, [][]string{{"echo", "head: $(git log -1 | Select-Object -First 1)"}}},
		{"ls | ForEach-Object { $_.Name; 'x}' }", [][]string{{"ls"}, {"ForEach-Object", "{ $_.Name; 'x}' }"}}},
		{"(Get-Date).Year", [][]string{{"(Get-Date).Year"}}},
		{"$h = @{ a = 1 }", [][]string{{"$h", "=", "@{ a = 1 }"}}},
		{"make 2>&1 | less", [][]string{{"make", "2>&1"}, {"less"}}},
		{"& 'C:\\Program Files\\x.exe' -v", [][]string{{"&", "C:\\Program Files\\x.exe", "-v"}}},
		{"Start-Sleep 5 &", [][]string{{"Start-Sleep", "5"}}},
		{"git status # check", [][]string{{"git", "status"}}},
		{"git <# inline #> status", [][]string{{"git", "status"}}},
		{"git `\n  status", [][]string{{"git", "status"}}},
		{"echo a`#b", [][]string{{"echo", "a#b"}}},
		{"git log\ngit diff", [][]string{{"git", "log"}, {"git", "diff"}}},
		{"echo ‘smart’ “quotes”", [][]string{{"echo", "smart", "quotes"}}},
		{"echo 'unterminated", [][]string{{"echo", "unterminated"}}},
	}
	for _, tt := range tests {
		if got := texts(SplitPwsh(tt.line)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitPwsh(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestStripPwsh(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{"& git status", []string{"git", "status"}},
		{". ./env.ps1", []string{"./env.ps1"}},
		{"sudo -E apt update", []string{"apt", "update"}},
		{"$out = git status", []string{"git", "status"}},
		{"$x=git", nil},
		{"foreach ($x in 1..3) { $x }", nil},
		{"If ($a) { b }", nil},
		{"if($a){ b }", nil},
		{"'quoted' foo", []string{"quoted", "foo"}},
	}
	for _, tt := range tests {
		cmds := SplitPwsh(tt.line)
		got := StripPwsh(cmds[0]).Texts()
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("StripPwsh(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestQuotedWordsKeepRaw(t *testing.T) {
	c := SplitPwsh(`"git" status`)[0]
	if c[0].Raw != `"git"` || c[0].Text != "git" {
		t.Errorf("got %+v", c[0])
	}
}
