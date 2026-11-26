package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	t.Setenv("SHTICK_SHELL", "zsh")
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

func TestHookHistoryEntry(t *testing.T) {
	defs := setup(t)
	dir := t.TempDir()
	for entry, want := range map[string]string{
		"   12  git status\n":                       "shtick: gst = git status\n",
		"123456* git checkout -b x\n":               "shtick: gco = git checkout\n",
		"    7  for b in a c; do\n  gco $b\ndone\n": "",
		"    8   git status\n":                      "shtick: gst = git status\n", // leading space kept
		"not history output\n":                      "",
	} {
		file := filepath.Join(dir, "entry")
		os.WriteFile(file, []byte(entry), 0o600)
		_, stderr, code := call(t, time.Now(), "hook", "--defs", defs, "--history-entry", file)
		if code != 0 || stderr != want {
			t.Errorf("%q: code %d stderr %q, want %q", entry, code, stderr, want)
		}
	}
	out, _, _ := call(t, time.Now(), "top", "--defs", defs)
	if !strings.Contains(out, "gco   function  1") {
		t.Errorf("for loop body should count gco:\n%s", out)
	}
	if _, stderr, code := call(t, time.Now(), "hook", "--defs", defs, "--history-entry", filepath.Join(dir, "missing")); code != 0 || stderr != "" {
		t.Errorf("missing entry file: code %d stderr %q", code, stderr)
	}
}

func TestImportBashHistory(t *testing.T) {
	defs := setup(t)
	t.Setenv("SHTICK_SHELL", "bash")
	hist := filepath.Join(t.TempDir(), "hist")
	os.WriteFile(hist, []byte("#100\ngst\n#200\ngit status\n#300\ngco main\n"), 0o600)
	out, stderr, code := call(t, time.Unix(500, 0), "import", "--defs", defs, "--histfile", hist)
	if code != 0 || !strings.Contains(out, "imported 3 events from 3 history entries") {
		t.Fatalf("import: %d %q %q", code, out, stderr)
	}
	out, _, _ = call(t, time.Unix(500, 0), "report", "--defs", defs)
	for _, want := range []string{"gst   alias     1     6m ago", "gco   function  1     3m ago", "gst   alias  1       1"} {
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
	out, _, code = call(t, time.Now(), "init", "bash")
	if code != 0 || !strings.Contains(out, "PS0=") {
		t.Errorf("init bash: %d", code)
	}
	if _, _, code := call(t, time.Now(), "init", "fish"); code == 0 {
		t.Error("init fish should fail")
	}
	if _, _, code := call(t, time.Now(), "frobnicate"); code != 2 {
		t.Error("unknown command should exit 2")
	}
}

// buildBin builds shtick and a stub git into a temp bin directory.
func buildBin(t *testing.T) (dir, bin string) {
	t.Helper()
	dir = t.TempDir()
	bin = filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "shtick"), ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho \"git $*\"\n"), 0o755)
	return dir, bin
}

// TestZsh runs the real plugin in an interactive zsh. Skipped when zsh is not
// installed.
func TestZsh(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	dir, bin := buildBin(t)
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

// TestBash runs the real plugin in an interactive bash, once with default
// history settings and once with ignoreboth. Skipped when bash is missing or
// older than 4.4.
func TestBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	if out, err := exec.Command(bash, "-c", "(( BASH_VERSINFO[0]*100 + BASH_VERSINFO[1] >= 404 ))").CombinedOutput(); err != nil {
		t.Skipf("bash older than 4.4: %s", out)
	}
	dir, bin := buildBin(t)
	plugin, _ := filepath.Abs("shell/shtick.bash")
	input := strings.Join([]string{
		"git status",
		"gco main",
		"git checkout -b x",
		"git fetch; git status",   // the sync function, not gst
		" git checkout -b hidden", // not saved under ignorespace
		"false",
		"echo after-false",
		"gst",
		"gst",
		"shtick top",
		"shtick missed",
		"shtick defs",
	}, "\n") + "\n"

	for _, hc := range []string{"", "ignoreboth"} {
		t.Run("HISTCONTROL="+hc, func(t *testing.T) {
			home := filepath.Join(dir, "home"+hc)
			os.MkdirAll(home, 0o755)
			rc := strings.Join([]string{
				"export PATH=" + bin + ":$PATH",
				"export SHTICK_DATA_DIR=" + filepath.Join(home, "data"),
				"export SHTICK_STATE_DIR=" + filepath.Join(home, "state"),
				"HISTFILE=" + filepath.Join(home, "hist"),
				"HISTCONTROL=" + hc,
				map[string]string{"ignoreboth": "set -u"}[hc], // the plugin must load under nounset
				"PS1='<$?>' PROMPT_COMMAND='history -a'",
				"alias gst='git status'",
				`gco() { git checkout "$@"; }`,
				"sync() { git fetch; git status; }",
				`mkcd() { mkdir -p "$1" && cd "$1"; }`,
				"source " + plugin,
			}, "\n") + "\n"
			rcfile := filepath.Join(home, ".bashrc")
			os.WriteFile(rcfile, []byte(rc), 0o644)

			cmd := exec.Command(bash, "--noprofile", "--rcfile", rcfile, "-i")
			cmd.Dir = home
			cmd.Env = append(os.Environ(), "HOME="+home, "BASH_ENV=", "SHTICK_NUDGE=", "SHTICK_DISABLE=", "SHTICK_IGNORE=", "SHTICK_SHELL=")
			cmd.Stdin = strings.NewReader(input)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("bash: %v\n%s", err, out)
			}
			got := string(out)
			hidden := 1 // the space-prefixed line is a missed gco unless ignorespace hid it
			gstUses := 2
			if hc == "ignoreboth" {
				hidden = 0
				gstUses = 1 // the repeat is not saved either, and can't be told apart
			}
			for _, want := range []string{
				"shtick: gst = git status",
				"shtick: gco = git checkout",
				"shtick: sync = git fetch; git status",
				"<1>echo after-false", // $? survives for PS1
				"gco   function  1",
				fmt.Sprintf("gst   alias     %d", gstUses),
				fmt.Sprintf("gco   function  %d       1", 1+hidden),
				"gst   alias     1       " + fmt.Sprint(gstUses), // missed once: only the bare git status
				"sync  function  1",
				"mkcd  function  no     ~/.bashrc",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("bash output missing %q:\n%s", want, got)
				}
			}
			if n := strings.Count(got, "shtick: gst = git status"); n != 1 {
				t.Errorf("gst nudged %d times, want 1:\n%s", n, got)
			}
			left, _ := filepath.Glob(filepath.Join(home, "state", "defs", "*"))
			if len(left) != 0 {
				t.Errorf("files left after exit: %v", left)
			}
		})
	}
}

// TestBashPreexec checks that with bash-preexec loaded the plugin registers
// through its hook arrays instead of PS0. The stub stands in for the library,
// which calls preexec functions with the typed line and precmd functions
// before each prompt.
func TestBashPreexec(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	if out, err := exec.Command(bash, "-c", "(( BASH_VERSINFO[0]*100 + BASH_VERSINFO[1] >= 404 ))").CombinedOutput(); err != nil {
		t.Skipf("bash older than 4.4: %s", out)
	}
	dir, bin := buildBin(t)
	plugin, _ := filepath.Abs("shell/shtick.bash")
	rc := strings.Join([]string{
		"export PATH=" + bin + ":$PATH",
		"export SHTICK_DATA_DIR=" + filepath.Join(dir, "data"),
		"export SHTICK_STATE_DIR=" + filepath.Join(dir, "state"),
		"PS1='' PS0='' PROMPT_COMMAND='__bp_precmd'",
		"bash_preexec_imported=defined",
		"declare -a preexec_functions precmd_functions",
		`__bp_precmd() { local f; for f in "${precmd_functions[@]}"; do "$f"; done; }`,
		"alias gst='git status'",
		"source " + plugin,
	}, "\n") + "\n"
	rcfile := filepath.Join(dir, ".bashrc")
	os.WriteFile(rcfile, []byte(rc), 0o644)
	cmd := exec.Command(bash, "--noprofile", "--rcfile", rcfile, "-i")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+dir, "BASH_ENV=", "SHTICK_NUDGE=", "SHTICK_DISABLE=", "SHTICK_IGNORE=")
	cmd.Stdin = strings.NewReader(strings.Join([]string{
		`echo "hooks: ${preexec_functions[*]}|${precmd_functions[*]}|$PS0|$PROMPT_COMMAND"`,
		`for f in "${preexec_functions[@]}"; do "$f" "git status"; done`,
		"shtick missed",
	}, "\n") + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"hooks: _shtick_bp_preexec|_shtick_precmd||__bp_precmd\n",
		"shtick: gst = git status",
		"gst   alias  1       0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("bash output missing %q:\n%s", want, got)
		}
	}
}
