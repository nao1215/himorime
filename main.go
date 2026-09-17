// Command himorime checks whether command-line programs stay within their
// performance budgets: latency, throughput, CPU time and peak RSS, locally and
// in CI.
//
// It measures commands declared in a versioned YAML suite, compares them with
// each other or with a Git base revision, checks absolute performance budgets,
// and exits non-zero on a confirmed regression. See
// https://nao1215.github.io/himorime/ for the documentation.
package main

import (
	"os"

	"github.com/nao1215/himorime/internal/cli"
	"github.com/nao1215/himorime/internal/proc"
)

func main() {
	// Measured commands are started from a spawner: this executable started
	// again in spawner mode, which the proc package's init selects before
	// the rest of himorime initializes.
	proc.EnableSpawner()
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
