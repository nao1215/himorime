//go:build linux || darwin

// Package differential compares what himorime measures with what an
// independent tool measures for the same command.
//
// himorime reads peak RSS and CPU time from the kernel through its own
// spawner, which clears the peak it would otherwise pass on to the command
// (see internal/proc). A mistake there does not crash anything: it reports a
// number that looks plausible and is wrong, such as himorime's own heap
// folded into a small command's peak, a unit off by 1024, or the CPU time of
// a different process. /usr/bin/time (GNU time on Linux, BSD time on macOS)
// reads the same kernel counters through wait4 in a process that does
// nothing else, so the two should agree within the noise of the command.
package differential

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const runs = 5

// workload is one command measured by both tools, run with the program in
// testdata/workload. The large allocation comes before the small ones, so a
// peak carried over from an earlier command shows up as a small command
// reporting a large peak.
type workload struct {
	name string
	args []string
}

var workloads = []workload{
	{name: "alloc 192MiB", args: []string{"alloc", "192"}},
	{name: "alloc 1MiB", args: []string{"alloc", "1"}},
	{name: "alloc 32MiB", args: []string{"alloc", "32"}},
	{name: "spin 300M rounds", args: []string{"spin", "300000000"}},
}

// usage is the median of the runs of one workload.
type usage struct {
	peakRSS int64 // bytes
	floor   int64 // bytes; himorime only: the peak the command may have inherited
	user    time.Duration
	system  time.Duration
}

func TestPeakRSSAndCPUMatchTime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds himorime and measures real commands")
	}
	if _, err := os.Stat("/usr/bin/time"); err != nil {
		// GitHub's Linux and macOS runners have it; a CI run that skipped
		// here would pass without comparing anything.
		if os.Getenv("CI") == "true" {
			t.Fatalf("/usr/bin/time is missing on CI: %v", err)
		}
		t.Skip("/usr/bin/time is not installed")
	}
	dir := t.TempDir()
	himorime := build(t, dir, "himorime", "../..")
	helper := build(t, dir, "workload", "./testdata/workload")

	got := measureWithHimorime(t, dir, himorime, helper)
	for _, w := range workloads {
		want := measureWithTime(t, helper, w.args)
		h := got[w.name]
		t.Logf("%s: himorime peak %.1fMiB (floor %.1fMiB) user %v system %v, time peak %.1fMiB user %v system %v",
			w.name, mib(h.peakRSS), mib(h.floor), h.user, h.system, mib(want.peakRSS), want.user, want.system)

		// Linux and macOS fold the RSS of the process that starts a command
		// into the command's peak, and himorime's starter is larger than
		// time. himorime reports that amount as the floor and marks a peak at
		// or below it as unknown; all it then claims is that the command used
		// at most the floor, which time must not contradict.
		const slack = 4 << 20
		switch {
		case h.floor <= 0:
			t.Errorf("%s: no measurement floor reported", w.name)
		case h.peakRSS <= h.floor:
			if want.peakRSS > h.floor+slack {
				t.Errorf("%s: peak RSS at the floor of %.1fMiB, but /usr/bin/time measured %.1fMiB", w.name, mib(h.floor), mib(want.peakRSS))
			}
		default:
			// The two agree to a fraction of a MiB on Linux. Two MiB or 1%
			// covers the runtime's variation between runs and still catches a
			// kilobyte taken as 1000 bytes (2.4%), let alone a peak that
			// includes himorime's own heap.
			if d := abs(h.peakRSS - want.peakRSS); d > max(2<<20, want.peakRSS/100) {
				t.Errorf("%s: peak RSS %.1fMiB, /usr/bin/time %.1fMiB", w.name, mib(h.peakRSS), mib(want.peakRSS))
			}
		}
		// time prints CPU time in hundredths of a second, and a busy machine
		// adds some system time to the same amount of work.
		// User and system time are compared apart, so swapping them is caught.
		if d := absDur(h.user - want.user); d > max(50*time.Millisecond, want.user/5) {
			t.Errorf("%s: user CPU time %v, /usr/bin/time %v", w.name, h.user, want.user)
		}
		if d := absDur(h.system - want.system); d > max(50*time.Millisecond, want.system/5) {
			t.Errorf("%s: system CPU time %v, /usr/bin/time %v", w.name, h.system, want.system)
		}
	}
}

func build(t *testing.T, dir, name, pkg string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
	return out
}

func measureWithHimorime(t *testing.T, dir, himorime, helper string) map[string]usage {
	t.Helper()
	var suite strings.Builder
	fmt.Fprintf(&suite, "version: \"1\"\nname: differential\ndefaults: {warmup: 0, runs: %d}\nbenchmarks:\n", runs)
	for _, w := range workloads {
		args := append([]string{helper}, w.args...)
		quoted, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&suite, "  - name: %q\n    metrics: {memory: true, cpu: true}\n    commands:\n      app:\n        command: %s\n", w.name, quoted)
	}
	suitePath := filepath.Join(dir, "himorime.yaml")
	if err := os.WriteFile(suitePath, []byte(suite.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(dir, "report.json")
	cmd := exec.Command(himorime, "run", "--quiet", "--format", "json", "--output", reportPath, suitePath)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("himorime run: %v\n%s", err, b)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	type stat struct {
		Status string `json:"status"`
		Unit   string `json:"unit"`
		Floor  int64  `json:"floor"`
		Stats  *struct {
			Median float64 `json:"median"`
		} `json:"stats"`
	}
	var report struct {
		Suites []struct {
			Benchmarks []struct {
				Name     string `json:"name"`
				Commands []struct {
					Head struct {
						Metrics map[string]stat `json:"metrics"`
					} `json:"head"`
				} `json:"commands"`
			} `json:"benchmarks"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Suites) != 1 {
		t.Fatalf("report has %d suites", len(report.Suites))
	}
	median := func(name string, m map[string]stat, key, unit string) float64 {
		s, ok := m[key]
		if !ok || s.Status != "measured" || s.Unit != unit || s.Stats == nil {
			t.Fatalf("%s: %s is %+v, want measured in %s", name, key, s, unit)
		}
		return s.Stats.Median
	}
	got := map[string]usage{}
	for _, b := range report.Suites[0].Benchmarks {
		if len(b.Commands) != 1 {
			t.Fatalf("%s: %d commands", b.Name, len(b.Commands))
		}
		m := b.Commands[0].Head.Metrics
		got[b.Name] = usage{
			peakRSS: int64(median(b.Name, m, "peak_rss", "bytes")),
			floor:   m["peak_rss"].Floor,
			user:    time.Duration(median(b.Name, m, "cpu_user", "ns")),
			system:  time.Duration(median(b.Name, m, "cpu_system", "ns")),
		}
	}
	if len(got) != len(workloads) {
		t.Fatalf("report has %d benchmarks, want %d", len(got), len(workloads))
	}
	return got
}

func measureWithTime(t *testing.T, helper string, args []string) usage {
	t.Helper()
	var peaks []int64
	var users, systems []time.Duration
	for range runs {
		var u usage
		var err error
		if runtime.GOOS == "darwin" {
			u, err = bsdTime(helper, args)
		} else {
			u, err = gnuTime(helper, args)
		}
		if err != nil {
			t.Fatal(err)
		}
		peaks = append(peaks, u.peakRSS)
		users = append(users, u.user)
		systems = append(systems, u.system)
	}
	return usage{peakRSS: medianOf(peaks), user: medianOf(users), system: medianOf(systems)}
}

// gnuTime runs the command under GNU time, which prints ru_maxrss in KiB and
// the user and system time in seconds with two decimals.
func gnuTime(helper string, args []string) (usage, error) {
	out, err := os.CreateTemp("", "gnu-time-*")
	if err != nil {
		return usage{}, err
	}
	defer os.Remove(out.Name())
	_ = out.Close()
	cmd := exec.Command("/usr/bin/time", append([]string{"-o", out.Name(), "-f", "%M %U %S", helper}, args...)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		return usage{}, fmt.Errorf("/usr/bin/time %v: %w\n%s", args, err, b)
	}
	data, err := os.ReadFile(out.Name())
	if err != nil {
		return usage{}, err
	}
	f := strings.Fields(string(data))
	if len(f) != 3 {
		return usage{}, fmt.Errorf("unexpected GNU time output %q", data)
	}
	kib, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return usage{}, err
	}
	user, err := strconv.ParseFloat(f[1], 64)
	if err != nil {
		return usage{}, err
	}
	sys, err := strconv.ParseFloat(f[2], 64)
	if err != nil {
		return usage{}, err
	}
	return usage{peakRSS: kib << 10, user: seconds(user), system: seconds(sys)}, nil
}

// bsdTime runs the command under macOS's time -l, which prints
// "<real> real <user> user <sys> sys" and ru_maxrss in bytes on a line
// ending in "maximum resident set size".
func bsdTime(helper string, args []string) (usage, error) {
	cmd := exec.Command("/usr/bin/time", append([]string{"-l", helper}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return usage{}, fmt.Errorf("/usr/bin/time -l %v: %w\n%s", args, err, stderr.Bytes())
	}
	var u usage
	var haveCPU, haveRSS bool
	sc := bufio.NewScanner(&stderr)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch {
		case len(f) == 6 && f[1] == "real" && f[3] == "user" && f[5] == "sys":
			user, err1 := strconv.ParseFloat(f[2], 64)
			sys, err2 := strconv.ParseFloat(f[4], 64)
			if err1 != nil || err2 != nil {
				return usage{}, fmt.Errorf("unexpected time line %q", sc.Text())
			}
			u.user, u.system, haveCPU = seconds(user), seconds(sys), true
		case strings.HasSuffix(sc.Text(), "maximum resident set size") && len(f) > 0:
			v, err := strconv.ParseInt(f[0], 10, 64)
			if err != nil {
				return usage{}, fmt.Errorf("unexpected time line %q", sc.Text())
			}
			u.peakRSS, haveRSS = v, true
		}
	}
	if !haveCPU || !haveRSS {
		return usage{}, fmt.Errorf("time -l printed no CPU time or peak RSS:\n%s", stderr.String())
	}
	return u, nil
}

func seconds(s float64) time.Duration { return time.Duration(math.Round(s * float64(time.Second))) }

func medianOf[T int64 | time.Duration](v []T) T {
	s := slices.Clone(v)
	slices.Sort(s)
	return s[len(s)/2]
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func mib(b int64) float64 { return float64(b) / (1 << 20) }
