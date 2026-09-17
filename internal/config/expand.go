package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Variable names a suite may reference with ${name}.
const (
	VarArtifact  = "artifact"
	VarRoot      = "root"
	VarHeadRoot  = "head_root"
	VarWorkdir   = "workdir"
	VarExe       = "exe"
	varEnvPrefix = "env:"
)

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Ref is one ${...} reference inside a template.
type Ref struct {
	Name string // artifact, root, head_root, workdir, exe, or env
	Env  string // the variable name for ${env:NAME}
}

// References parses every ${...} reference in s. "$${" is an escaped literal
// "${" and is not a reference. An unterminated or unknown reference is an error.
func References(s string) ([]Ref, error) {
	var refs []Ref
	_, err := expand(s, func(r Ref) (string, error) {
		refs = append(refs, r)
		return "", nil
	})
	return refs, err
}

// Vars are the values substituted into a template.
type Vars struct {
	Artifact string
	// Root is the suite directory inside the tree being measured.
	Root string
	// HeadRoot is the suite directory inside the working tree, the same for
	// every side of a comparison.
	HeadRoot string
	Workdir  string
	Exe      string
	// LookupEnv resolves ${env:NAME}. It is os.LookupEnv in production.
	LookupEnv func(string) (string, bool)
}

// Expand substitutes every reference in s. quote, when non-nil, is applied to
// each substituted value; the runner passes a shell quoting function for
// `shell: true` commands so that a value can never become shell syntax.
func Expand(s string, v Vars, quote func(string) (string, error)) (string, error) {
	return expand(s, func(r Ref) (string, error) {
		val, err := v.value(r)
		if err != nil {
			return "", err
		}
		if quote != nil {
			return quote(val)
		}
		return val, nil
	})
}

func (v Vars) value(r Ref) (string, error) {
	switch r.Name {
	case VarArtifact:
		if v.Artifact == "" {
			return "", fmt.Errorf("${artifact} is only available when the suite has a build section")
		}
		return v.Artifact, nil
	case VarRoot:
		return v.Root, nil
	case VarHeadRoot:
		return v.HeadRoot, nil
	case VarWorkdir:
		if v.Workdir == "" {
			return "", fmt.Errorf("${workdir} is not available in the build step")
		}
		return v.Workdir, nil
	case VarExe:
		return v.Exe, nil
	default:
		if v.LookupEnv == nil {
			return "", fmt.Errorf("environment variable %s is not set", r.Env)
		}
		val, ok := v.LookupEnv(r.Env)
		if !ok {
			return "", fmt.Errorf("environment variable %s is not set; export it or remove ${env:%s}", r.Env, r.Env)
		}
		return val, nil
	}
}

func expand(s string, resolve func(Ref) (string, error)) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "$${") {
			b.WriteString("${")
			i += 3
			continue
		}
		if !strings.HasPrefix(s[i:], "${") {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i+2:], '}')
		if end < 0 {
			return "", fmt.Errorf("unterminated variable reference in %q: close it with } or write $${ for a literal ${", s)
		}
		name := s[i+2 : i+2+end]
		ref, err := parseRef(name)
		if err != nil {
			return "", err
		}
		val, err := resolve(ref)
		if err != nil {
			return "", err
		}
		b.WriteString(val)
		i += 2 + end + 1
	}
	return b.String(), nil
}

func parseRef(name string) (Ref, error) {
	switch name {
	case VarArtifact, VarRoot, VarHeadRoot, VarWorkdir, VarExe:
		return Ref{Name: name}, nil
	}
	if env, ok := strings.CutPrefix(name, varEnvPrefix); ok {
		if !envNameRE.MatchString(env) {
			return Ref{}, fmt.Errorf("invalid environment variable name in ${%s}", name)
		}
		return Ref{Name: "env", Env: env}, nil
	}
	return Ref{}, fmt.Errorf("unknown variable ${%s}: use ${artifact}, ${root}, ${head_root}, ${workdir}, ${exe} or ${env:NAME}", name)
}

// QuotedReference reports whether a shell script places a ${...} reference
// inside quotes it writes itself. himorime quotes every substituted value for
// the shell, and a second, surrounding layer of quotes changes what those
// quotes mean: inside "..." a single quote is an ordinary character, so a value
// holding $(...) would run. Such scripts are rejected instead.
func QuotedReference(script string) bool {
	var quote byte
	for i := 0; i < len(script); i++ {
		c := script[i]
		switch {
		case quote != '\'' && c == '\\' && i+1 < len(script):
			i++
		case strings.HasPrefix(script[i:], "$${"):
			i += 2
		case strings.HasPrefix(script[i:], "${"):
			if quote != 0 {
				return true
			}
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case quote != 0 && c == quote:
			quote = 0
		}
	}
	return false
}
