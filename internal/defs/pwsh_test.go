package defs

import "testing"

func TestParsePwsh(t *testing.T) {
	data := dump(
		[4]string{"shell", "pwsh", "", ""},
		[4]string{"alias", "k", "", "kubectl"},
		[4]string{"alias", "gci", "builtin", "Get-ChildItem"},
		[4]string{"function", "gst", `C:\Users\z\Documents\PowerShell\profile.ps1`, " git status @args "},
		[4]string{"function", "prompt", "", "..."},
		[4]string{"function", "C:", "", "Set-Location C:"},
		[4]string{"function", "Clear-Host", "builtin", "..."},
		[4]string{"function", "Write-VcsStatus", `C:\Users\z\Documents\PowerShell\Modules\posh-git\1.1.0\posh-git.psm1`, "..."},
		[4]string{"function", "_shtick_hook", "", "..."},
		[4]string{"function", "shtick", "", "..."},
	)
	s, err := Parse(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Shell != Pwsh {
		t.Fatalf("shell = %q", s.Shell)
	}
	if len(s.Aliases) != 1 || len(s.Functions) != 1 {
		t.Errorf("tracked aliases %v functions %v", s.Aliases, s.Functions)
	}
	if s.Functions["gst"].Shell != Pwsh {
		t.Error("defs should carry their shell")
	}
	reasons := map[string]string{}
	for _, sk := range s.Skipped {
		reasons[sk.Name] = sk.Reason
	}
	for name, want := range map[string]string{
		"gci":             "PowerShell built-in",
		"prompt":          "PowerShell default",
		"C:":              "drive function",
		"Clear-Host":      "PowerShell built-in",
		"Write-VcsStatus": "module source */Modules/*",
		"_shtick_hook":    "shtick itself",
		"shtick":          "shtick itself",
	} {
		if reasons[name] != want {
			t.Errorf("%s skipped for %q, want %q", name, reasons[name], want)
		}
	}
	if !s.Has("GST") || !s.Has("K") || s.Has("nope") {
		t.Error("Has should ignore case in PowerShell")
	}
	if !s.Defined("GCI") || s.Defined("nope") {
		t.Error("Defined should ignore case and include skipped names")
	}
}

func TestParseWithoutShellIsZsh(t *testing.T) {
	s, err := Parse(dump([4]string{"alias", "gst", "", "git status"}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Shell != Zsh || s.Has("GST") {
		t.Errorf("shell %q, case-insensitive %v", s.Shell, s.Has("GST"))
	}
}
