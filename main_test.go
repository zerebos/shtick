package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeDefs(t *testing.T, dir string, recs ...[4]string) string {
	t.Helper()
	var b bytes.Buffer
	for _, r := range recs {
		for _, f := range r {
			b.WriteString(f)
			b.WriteByte(0)
		}
	}
	file := filepath.Join(dir, "1.defs")
	if err := os.WriteFile(file, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func setup(t *testing.T) (defsFile string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SHTICK_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("SHTICK_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("SHTICK_NUDGE", "")
	t.Setenv("SHTICK_IGNORE", "")
	return writeDefs(t, dir,
		[4]string{"alias", "gst", "", "git status"},
		[4]string{"function", "gco", "/home/z/.zshrc", "\tgit checkout \"$@\""},
		[4]string{"function", "never", "/home/z/.zshrc", "\techo hi"},
	)
}

func call(t *testing.T, now time.Time, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb, now)
	return out.String(), errb.String(), code
}

func TestHookNudgesAndRecords(t *testing.T) {
	defs := setup(t)
	now := time.Unix(1_800_000_000, 0)
	_, stderr, code := call(t, now, "hook", "--defs", defs, "--", "git status")
	if code != 0 || stderr != "shtick: gst = git status\n" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	t.Setenv("SHTICK_NUDGE", "0")
	if _, stderr, _ := call(t, now, "hook", "--defs", defs, "--", "git checkout -b x"); stderr != "" {
		t.Errorf("quiet mode printed %q", stderr)
	}
	call(t, now, "hook", "--defs", defs, "--", "gco main")

	out, _, code := call(t, now, "report", "--defs", defs)
	if code != 0 {
		t.Fatal(out)
	}
	for _, want := range []string{"gco   function  1", "never  function  { echo hi }", "gst   alias     1       0", "gco   function  1       1"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func TestHookNeverFails(t *testing.T) {
	setup(t)
	t.Setenv("SHTICK_DEFS", "/nonexistent/defs")
	if _, stderr, code := call(t, time.Now(), "hook", "--", "git status"); code != 0 || stderr != "" {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

func TestImportOnlyBeforeLiveTracking(t *testing.T) {
	defs := setup(t)
	hist := filepath.Join(t.TempDir(), "hist")
	os.WriteFile(hist, []byte(": 100:0;gst\n: 200:0;git status\n: 300:0;gco main\n: 400:0;gst\n"), 0o600)

	// live tracking started at 300, so only entries before that import
	call(t, time.Unix(300, 0), "hook", "--defs", defs, "--", "gco main")
	call(t, time.Unix(400, 0), "hook", "--defs", defs, "--", "gst")

	out, stderr, code := call(t, time.Unix(500, 0), "import", "--defs", defs, "--histfile", hist)
	if code != 0 {
		t.Fatal(stderr)
	}
	if !strings.Contains(out, "imported 2 events from 2 history entries") {
		t.Errorf("import said %q", out)
	}
	if _, stderr, code := call(t, time.Unix(500, 0), "import", "--defs", defs, "--histfile", hist); code == 0 || !strings.Contains(stderr, "already imported") {
		t.Errorf("second import should refuse, got %d %q", code, stderr)
	}
	if _, stderr, code := call(t, time.Unix(500, 0), "import", "--defs", defs, "--histfile", hist, "--force"); code != 0 {
		t.Errorf("force import failed: %s", stderr)
	}
	out, _, _ = call(t, time.Unix(500, 0), "top", "--defs", defs)
	if !strings.Contains(out, "gst   alias     2") || !strings.Contains(out, "gco   function  1") {
		t.Errorf("top after import:\n%s", out)
	}
	out, _, _ = call(t, time.Unix(500, 0), "missed", "--defs", defs)
	if !strings.Contains(out, "gst   alias  1") {
		t.Errorf("missed after import:\n%s", out)
	}
}

func TestSuggestAndDefs(t *testing.T) {
	defs := setup(t)
	hist := filepath.Join(t.TempDir(), "hist")
	var b strings.Builder
	for i := 0; i < 15; i++ {
		b.WriteString("terraform plan -out tf\n")
	}
	os.WriteFile(hist, []byte(b.String()), 0o600)
	t.Setenv("PATH", t.TempDir())
	out, stderr, code := call(t, time.Now(), "suggest", "--defs", defs, "--histfile", hist)
	if code != 0 || !strings.Contains(out, "alias tpo='terraform plan -out'  # typed 15 times") {
		t.Errorf("suggest: %d %q %q", code, out, stderr)
	}
	out, _, _ = call(t, time.Now(), "defs", "--defs", defs)
	if !strings.Contains(out, "never  function  yes    /home/z/.zshrc") {
		t.Errorf("defs:\n%s", out)
	}
}

func TestInitAndUnknown(t *testing.T) {
	out, _, code := call(t, time.Now(), "init", "zsh")
	if code != 0 || !strings.Contains(out, "add-zsh-hook preexec _shtick_preexec") {
		t.Errorf("init zsh: %d", code)
	}
	if _, _, code := call(t, time.Now(), "init", "bash"); code == 0 {
		t.Error("init bash should fail for now")
	}
	if _, _, code := call(t, time.Now(), "frobnicate"); code != 2 {
		t.Error("unknown command should exit 2")
	}
}

// TestZsh runs the real plugin in an interactive zsh. Skipped when zsh is not
// installed.
func TestZsh(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "shtick"), ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho \"git $*\"\n"), 0o755)
	plugin, _ := filepath.Abs("shell/shtick.plugin.zsh")
	zdot := filepath.Join(dir, "zdot")
	os.MkdirAll(zdot, 0o755)
	rc := strings.Join([]string{
		"export PATH=" + bin + ":$PATH",
		"export SHTICK_DATA_DIR=" + filepath.Join(dir, "data"),
		"export SHTICK_STATE_DIR=" + filepath.Join(dir, "state"),
		"HISTFILE=" + filepath.Join(dir, "hist"),
		"PROMPT='' RPROMPT=''",
		"unsetopt prompt_sp",
		"alias gst='git status'",
		`gco() { git checkout "$@" }`,
		"source " + plugin,
	}, "\n") + "\n"
	os.WriteFile(filepath.Join(zdot, ".zshrc"), []byte(rc), 0o644)

	cmd := exec.Command(zsh, "-i")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ZDOTDIR="+zdot, "HOME="+dir)
	cmd.Stdin = strings.NewReader("git status\ngco main\ngit checkout -b x\nshtick top\nshtick unused\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"shtick: gst = git status",
		"shtick: gco = git checkout",
		"gco   function  1",
		"gst   alias  git status",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("zsh output missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "state", "defs")); err != nil {
		t.Errorf("defs dir: %v", err)
	}
}

func setupPwsh(t *testing.T) (defsFile, dir string) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv("SHTICK_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("SHTICK_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("SHTICK_NUDGE", "")
	t.Setenv("SHTICK_IGNORE", "")
	t.Setenv("SHTICK_LINE", "")
	return writeDefs(t, dir,
		[4]string{"shell", "pwsh", "", ""},
		[4]string{"alias", "k", "", "kubectl"},
		[4]string{"alias", "gci", "builtin", "Get-ChildItem"},
		[4]string{"function", "gst", "/home/z/profile.ps1", " git status @args "},
	), dir
}

func TestPwshHookReadsLineFromEnv(t *testing.T) {
	defs, _ := setupPwsh(t)
	t.Setenv("SHTICK_LINE", `Git status -s; k get pods`)
	stdout, stderr, code := call(t, time.Unix(1000, 0), "hook", "--defs", defs, "--stdout")
	if code != 0 || stderr != "" || stdout != "shtick: gst = git status\n" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	out, _, _ := call(t, time.Unix(1000, 0), "report", "--defs", defs)
	for _, want := range []string{"k     alias  1", "gst   function  1       0"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "gci") {
		t.Errorf("built-in alias reported:\n%s", out)
	}
}

func TestPwshImportStopsAtMark(t *testing.T) {
	defs, dir := setupPwsh(t)
	hist := filepath.Join(dir, "ConsoleHost_history.txt")
	before := "git status\r\nk get pods\r\n"
	os.WriteFile(hist, []byte(before+"gst\r\n"), 0o600)
	os.MkdirAll(filepath.Join(dir, "state"), 0o700)
	os.WriteFile(filepath.Join(dir, "state", "pwsh-history.mark"), []byte(strconv.Itoa(len(before))), 0o600)

	// a zsh import must not count as this shell's import
	os.MkdirAll(filepath.Join(dir, "data"), 0o700)
	os.WriteFile(filepath.Join(dir, "data", "usage.tsv"), []byte("50\talias\tgst\thistory\n"), 0o600)
	call(t, time.Unix(300, 0), "hook", "--defs", defs, "--", "gst")

	out, stderr, code := call(t, time.Unix(500, 0), "import", "--defs", defs, "--histfile", hist)
	if code != 0 || !strings.Contains(out, "imported 2 events from 2 history entries") {
		t.Fatalf("import: %d %q %q", code, out, stderr)
	}
	if _, stderr, code := call(t, time.Unix(500, 0), "import", "--defs", defs, "--histfile", hist); code == 0 || !strings.Contains(stderr, "already imported") {
		t.Errorf("second import should refuse, got %d %q", code, stderr)
	}
	call(t, time.Unix(500, 0), "import", "--defs", defs, "--histfile", hist, "--force")
	data, _ := os.ReadFile(filepath.Join(dir, "data", "usage.tsv"))
	if got := strings.Count(string(data), "\thistory-pwsh\n"); got != 2 {
		t.Errorf("want 2 pwsh history records after --force, got %d:\n%s", got, data)
	}
	if !strings.Contains(string(data), "50\talias\tgst\thistory\n") {
		t.Errorf("--force dropped the zsh import:\n%s", data)
	}
}

func TestPwshSuggestAndInit(t *testing.T) {
	defs, dir := setupPwsh(t)
	hist := filepath.Join(dir, "hist")
	os.WriteFile(hist, []byte(strings.Repeat("Get-ChildItem -Recurse -Force\n", 12)), 0o600)
	t.Setenv("PATH", t.TempDir())
	out, stderr, code := call(t, time.Now(), "suggest", "--defs", defs, "--histfile", hist)
	if code != 0 || !strings.Contains(out, "function grf { Get-ChildItem -Recurse -Force @args }  # typed 12 times") {
		t.Errorf("suggest: %d %q %q", code, out, stderr)
	}
	out, _, code = call(t, time.Now(), "init", "pwsh")
	if code != 0 || !strings.Contains(out, "Set-PSReadLineOption -AddToHistoryHandler") {
		t.Errorf("init pwsh: %d", code)
	}
}

// TestPwsh loads the real PowerShell plugin and drives its hook directly,
// since PSReadLine needs a terminal. Skipped when pwsh is not installed.
func TestPwsh(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not installed")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	exe := "shtick"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", filepath.Join(bin, exe), ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	plugin, _ := filepath.Abs("shell/shtick.plugin.ps1")
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := strings.Join([]string{
		"$env:PATH = " + q(bin+string(os.PathListSeparator)) + " + $env:PATH",
		"$env:SHTICK_DATA_DIR = " + q(filepath.Join(dir, "data")),
		"$env:SHTICK_STATE_DIR = " + q(filepath.Join(dir, "state")),
		"function gst { git status @args }",
		"function Get-Thing { param($Name) Get-Item $Name }",
		"Set-Alias k kubectl",
		". " + q(plugin),
		"$global:LASTEXITCODE = 7",
		"_shtick_hook 'Git status -s'",
		"_shtick_hook 'k get pods'",
		`"exit code kept: $LASTEXITCODE"`,
		"shtick top",
		"shtick defs",
	}, "\n") + "\n"
	file := filepath.Join(dir, "test.ps1")
	os.WriteFile(file, []byte(script), 0o644)

	cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File", file)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pwsh: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		`shtick: gst = git status`,
		`exit code kept: 7`,
		`(?m)^k\s+alias\s+1\b`,
		`(?m)^Get-Thing\s+function\s+no\b`,
		`(?m)^gst\s+function\s+yes\b`,
	} {
		if !regexp.MustCompile(want).MatchString(got) {
			t.Errorf("pwsh output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Clear-Host", "TabExpansion2", "_shtick_hook"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("pwsh defs should skip %s:\n%s", unwanted, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "state", "defs")); err != nil {
		t.Errorf("defs dir: %v", err)
	}
}
