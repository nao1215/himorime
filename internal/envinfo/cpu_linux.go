//go:build linux

package envinfo

import (
	"bufio"
	"os"
	"strings"
)

func cpuModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "model name", "Model", "cpu model", "Hardware":
			if v := strings.TrimSpace(val); v != "" {
				return v
			}
		}
	}
	return ""
}
