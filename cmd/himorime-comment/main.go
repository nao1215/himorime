// Command himorime-comment publishes a validated himorime JSON report as a
// pull request comment from a trusted GitHub Actions workflow_run job.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/nao1215/himorime/internal/prcomment"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := prcomment.Main(ctx, os.Args[1:], os.Stdout, os.Stderr, prcomment.Dependencies{})
	stop()
	os.Exit(code)
}
