package config

import (
	"errors"
	"strings"
	"testing"
)

func TestExpand(t *testing.T) {
	t.Parallel()
	vars := Vars{
		Artifact: "/tmp/a/artifact",
		Root:     "/repo",
		HeadRoot: "/work",
		Workdir:  "/tmp/w",
		Exe:      ".exe",
		LookupEnv: func(k string) (string, bool) {
			if k == "HOME_DIR" {
				return "/home/me", true
			}
			return "", false
		},
	}
	tests := map[string]string{
		"plain":                      "plain",
		"${artifact}":                "/tmp/a/artifact",
		"${root}/testdata/x.txt":     "/repo/testdata/x.txt",
		"${workdir}/out${exe}":       "/tmp/w/out.exe",
		"home=${env:HOME_DIR}":       "home=/home/me",
		"$${artifact} stays literal": "${artifact} stays literal",
		"a $ b $1 {x}":               "a $ b $1 {x}",
		"${root}${root}":             "/repo/repo",
		"${head_root}/testdata/x":    "/work/testdata/x",
	}
	for in, want := range tests {
		got, err := Expand(in, vars, nil)
		if err != nil || got != want {
			t.Errorf("Expand(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestExpandErrors(t *testing.T) {
	t.Parallel()
	vars := Vars{Root: "/r", LookupEnv: func(string) (string, bool) { return "", false }}
	for in, want := range map[string]string{
		"${nope}":        "unknown variable ${nope}",
		"${root":         "unterminated",
		"${env:MISSING}": "MISSING is not set",
		"${env:1BAD}":    "invalid environment variable name",
		"${artifact}":    "only available when the suite has a build",
		"${workdir}":     "not available in the build step",
	} {
		_, err := Expand(in, vars, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Expand(%q) error = %v, want it to mention %q", in, err, want)
		}
	}
}

func TestExpandQuotesEverySubstitution(t *testing.T) {
	t.Parallel()
	vars := Vars{Root: "/a b", Workdir: "/w"}
	quote := func(s string) (string, error) { return "<" + s + ">", nil }
	got, err := Expand("cd ${root} && ls ${workdir}", vars, quote)
	if err != nil || got != "cd </a b> && ls </w>" {
		t.Fatalf("Expand = %q, %v", got, err)
	}
	refuse := func(string) (string, error) { return "", errors.New("unquotable") }
	if _, err := Expand("x ${root}", vars, refuse); err == nil {
		t.Fatal("a quoting error was ignored")
	}
}

func TestReferences(t *testing.T) {
	t.Parallel()
	refs, err := References("${artifact} --in ${root}/x ${env:TOKEN} $${literal}")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[0].Name != VarArtifact || refs[1].Name != VarRoot || refs[2].Env != "TOKEN" {
		t.Fatalf("References = %+v", refs)
	}
}

func TestQuotedReference(t *testing.T) {
	t.Parallel()
	for script, want := range map[string]bool{
		"cat ${workdir}/in.txt | wc -l":    false,
		`echo "${env:BRANCH}"`:             true,
		`echo '${root}'`:                   true,
		`echo "a" ${root} "b"`:             false,
		`echo "$${literal}"`:               false,
		`echo \"${root}\"`:                 false,
		`echo "it's" ${root}`:              false,
		`echo 'say "hi"' ${root} "${exe}"`: true,
	} {
		if got := QuotedReference(script); got != want {
			t.Errorf("QuotedReference(%q) = %v, want %v", script, got, want)
		}
	}
}
