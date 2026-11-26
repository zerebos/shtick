package defs

import (
	"path"
	"regexp"
	"strings"
)

// DefaultPwshIgnoreSources are module and system paths whose PowerShell
// functions are not the user's own. Sources are matched with forward slashes,
// so these also cover Windows paths.
var DefaultPwshIgnoreSources = []string{
	"*/Modules/*", "*/oh-my-posh/*", "*/posh-git/*", "*/PSReadLine/*",
	"*/WindowsPowerShell/*", "/opt/microsoft/powershell/*",
	"/usr/local/microsoft/powershell/*", "*/scoop/*",
}

// pwshBuiltin is the source the plugin gives aliases and functions that
// PowerShell defines on its own.
const pwshBuiltin = "builtin"

// pwshDefaults are functions PowerShell or common hosts define, for when the
// plugin could not tell built-ins apart.
var pwshDefaults = map[string]bool{
	"prompt": true, "tabexpansion2": true, "psconsolehostreadline": true,
	"clear-host": true, "help": true, "oss": true, "pause": true, "mkdir": true,
	"more": true, "cd..": true, `cd\`: true, "cd~": true, "importsystemmodules": true,
	"get-verb": true,
}

var driveFunction = regexp.MustCompile(`^[A-Za-z]:$`)

func pwshSkipReason(d Def, names []string, sources []glob, cache map[string]string) string {
	if d.Source == pwshBuiltin {
		return "PowerShell built-in"
	}
	for _, g := range names {
		if ok, _ := path.Match(g, d.Name); ok {
			return "matches ignore " + g
		}
	}
	lower := strings.ToLower(d.Name)
	switch {
	case d.Name == "":
		return "empty name"
	case lower == "shtick", strings.HasPrefix(d.Name, "_shtick"):
		return "shtick itself"
	case d.Kind == Function && pwshDefaults[lower]:
		return "PowerShell default"
	case d.Kind == Function && driveFunction.MatchString(d.Name):
		return "drive function"
	}
	if d.Source == "" {
		return ""
	}
	if reason, ok := cache[d.Source]; ok {
		return reason
	}
	src := strings.ReplaceAll(d.Source, `\`, "/")
	reason := ""
	for _, g := range sources {
		if g.match(src) {
			reason = "module source " + g.pattern
			break
		}
	}
	cache[d.Source] = reason
	return reason
}
