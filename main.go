// Command yahiko is a CI-first benchmark runner for command-line programs.
//
// It measures commands declared in a versioned YAML suite, compares them with
// each other or with a Git base revision, checks absolute performance budgets,
// and exits non-zero on a confirmed regression. See
// https://nao1215.github.io/yahiko/ for the documentation.
package main

import (
	"os"

	"github.com/nao1215/yahiko/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
