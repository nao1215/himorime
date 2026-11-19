// Command workload is the program the differential test measures. Unlike the
// E2E helper, its CPU work is a fixed number of iterations instead of a
// wall-clock deadline, so its CPU time does not drop when the machine is
// busy, and both tools see the same amount of work.
//
//	workload alloc MIB   touch every page of a MIB buffer and hold it briefly
//	workload spin N      run N rounds of a xorshift loop on one thread
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: workload alloc MIB | workload spin N")
		os.Exit(2)
	}
	n, err := strconv.Atoi(os.Args[2])
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "invalid count %q\n", os.Args[2])
		os.Exit(2)
	}
	switch os.Args[1] {
	case "alloc":
		buf := make([]byte, n<<20)
		for i := 0; i < len(buf); i += 4096 {
			buf[i] = 1
		}
		time.Sleep(20 * time.Millisecond)
		runtime.KeepAlive(buf)
	case "spin":
		x := uint64(88172645463325252)
		for range n {
			x ^= x << 13
			x ^= x >> 7
			x ^= x << 17
		}
		if x == 0 {
			fmt.Println(x)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", os.Args[1])
		os.Exit(2)
	}
}
