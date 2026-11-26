package match

import (
	"regexp"
	"strings"

	"github.com/zerebos/shtick/internal/defs"
	"github.com/zerebos/shtick/internal/shparse"
)

// rules say which function bodies are plain templates in one shell.
type rules struct {
	dialect shparse.Dialect
	// argsTail is a final word that passes the caller's arguments through.
	argsTail *regexp.Regexp
	// positional is any other use of the arguments, which makes a body
	// more than a template.
	positional *regexp.Regexp
	// control words start statements that are not plain commands.
	control map[string]bool
	// controlPrefix lists first runes that mark expressions, not commands.
	controlPrefix string
}

func (r rules) isControl(w shparse.Word) bool {
	t := w.Text
	if r.dialect.CaseInsensitive {
		t = shparse.PwshKeyword(w)
	}
	if r.control[t] {
		return true
	}
	return r.controlPrefix != "" && w.Raw != "" && strings.ContainsRune(r.controlPrefix, []rune(w.Raw)[0])
}

var zshRules = rules{
	dialect:    shparse.Zsh,
	argsTail:   regexp.MustCompile(`^"?\$(@|\*|\{@\}|\{\*\}|argv|\{argv\[@\]\})"?$`),
	positional: regexp.MustCompile(`\$(\{#?[0-9#*@]|[0-9#*@]|\{?argv)`),
	control: map[string]bool{
		"if": true, "for": true, "foreach": true, "while": true, "until": true,
		"case": true, "select": true, "repeat": true, "local": true,
		"typeset": true, "declare": true, "return": true, "shift": true,
		"[[": true, "[": true, "test": true, "setopt": true, "emulate": true,
		"zparseopts": true, "{": true, "}": true, "then": true, "fi": true,
		"do": true, "done": true, "elif": true, "else": true, "esac": true,
	},
}

// pwshRules: a body ending in @args or $args forwards its arguments. Any
// other use of $args, $input, the pipeline variable, or bound parameters is
// not a template, and neither are param blocks, attributes like
// [CmdletBinding()], assignments, or keywords.
var pwshRules = rules{
	dialect:    shparse.Pwsh,
	argsTail:   regexp.MustCompile(`(?i)^"?[@$](args|\{args\})"?$`),
	positional: regexp.MustCompile(`(?i)[$@](args|\{args\}|input|_|psitem|psboundparameters|myinvocation)\b`),
	control: map[string]bool{
		"if": true, "elseif": true, "else": true, "for": true, "foreach": true,
		"while": true, "do": true, "until": true, "switch": true, "try": true,
		"catch": true, "finally": true, "trap": true, "throw": true, "return": true,
		"break": true, "continue": true, "exit": true, "param": true, "begin": true,
		"process": true, "end": true, "clean": true, "dynamicparam": true,
		"function": true, "filter": true, "class": true, "enum": true,
		"using": true, "data": true, "set-strictmode": true,
	},
	controlPrefix: "$[(@{",
}

func rulesFor(sh defs.Shell) rules {
	if sh == defs.Pwsh {
		return pwshRules
	}
	return zshRules
}
