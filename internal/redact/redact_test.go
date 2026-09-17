package redact

import "testing"

func TestRedactorMasksSecretLookingVariables(t *testing.T) {
	t.Parallel()
	env := []string{
		"GITHUB_TOKEN=ghp_abcdefghijklmnop",
		"MY_API_KEY=key-1234567",
		"DB_PASSWORD=hunter22",
		"HOME=/home/runner",
		"SHORT_TOKEN=abc",
		"malformed",
	}
	r := New(env, "extra-secret-value")
	in := "token ghp_abcdefghijklmnop key key-1234567 pw hunter22 home /home/runner abc extra-secret-value"
	want := "token *** key *** pw *** home /home/runner abc ***"
	if got := r.String(in); got != want {
		t.Fatalf("String() = %q\nwant       %q", got, want)
	}
}

func TestRedactorLongestFirst(t *testing.T) {
	t.Parallel()
	r := New(nil, "secret1", "secret1-and-more")
	if got := r.String("x secret1-and-more y"); got != "x *** y" {
		t.Fatalf("got %q", got)
	}
}

func TestNilRedactor(t *testing.T) {
	t.Parallel()
	var r *Redactor
	if r.String("plain") != "plain" {
		t.Fatal("nil redactor changed text")
	}
}

func TestIsSecretName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"GITHUB_TOKEN": true, "aws_secret_access_key": true, "NPM_AUTH": true,
		"PATH": false, "HOME": false, "GOFLAGS": false,
	} {
		if got := IsSecretName(name); got != want {
			t.Errorf("IsSecretName(%s) = %v, want %v", name, got, want)
		}
	}
}

func TestAddAndMaxLen(t *testing.T) {
	t.Parallel()
	r := New(nil)
	if r.MaxLen() != 0 {
		t.Fatal("empty redactor")
	}
	r.Add("short")
	r.Add("added-secret-1234")
	r.Add("added-secret-1234")
	if r.MaxLen() != len("added-secret-1234") || r.String("x added-secret-1234") != "x ***" || r.String("short") != "short" {
		t.Fatalf("Add: %q, %d", r.String("x added-secret-1234"), r.MaxLen())
	}
	var nilR *Redactor
	nilR.Add("ignored-value")
	if nilR.MaxLen() != 0 {
		t.Fatal("nil MaxLen")
	}
}
