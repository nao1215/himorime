package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nao1215/himorime/internal/runner"
)

const progressRedrawInterval = 100 * time.Millisecond

// progressRenderer keeps one benchmark line on a terminal. It also wraps the
// regular stderr log so a log message cannot be printed in the middle of it.
type progressRenderer struct {
	out    io.Writer
	now    func() time.Time
	latest runner.Progress
	active bool
	closed bool
	last   time.Time
}

func newProgressRenderer(out io.Writer, now func() time.Time) *progressRenderer {
	if now == nil {
		now = time.Now
	}
	return &progressRenderer{out: out, now: now}
}

func (p *progressRenderer) Update(v runner.Progress) {
	if p.closed {
		return
	}
	previous := p.latest
	p.latest = v
	if !p.active || previous.Benchmark != v.Benchmark || p.now().Sub(p.last) >= progressRedrawInterval {
		p.draw()
	}
}

func (p *progressRenderer) Write(b []byte) (int, error) {
	if p.closed || !p.active {
		return p.out.Write(b)
	}
	if _, err := io.WriteString(p.out, "\r\033[2K"); err != nil {
		return 0, err
	}
	n, err := p.out.Write(b)
	if err != nil {
		return n, err
	}
	// Logs are line-oriented and must leave the current benchmark visible.
	p.draw()
	return n, nil
}

func (p *progressRenderer) draw() {
	if p.active {
		_, _ = io.WriteString(p.out, "\r\033[2K")
	}
	_, _ = io.WriteString(p.out, progressText(p.latest))
	p.active = true
	p.last = p.now()
}

func (p *progressRenderer) Close() {
	if p.closed {
		return
	}
	p.closed = true
	if p.active {
		_, _ = io.WriteString(p.out, "\r\033[2K\n")
		p.active = false
	}
}

func progressText(v runner.Progress) string {
	var b strings.Builder
	fmt.Fprintf(&b, "himorime: benchmark %q: warmup %d/%d, ", v.Benchmark, v.Warmups, v.WarmupTotal)
	if v.Runs > 0 {
		fmt.Fprintf(&b, "measured %d/%d", v.Rounds, v.Runs)
	} else {
		fmt.Fprintf(&b, "measured %d", v.Rounds)
	}
	return b.String()
}
