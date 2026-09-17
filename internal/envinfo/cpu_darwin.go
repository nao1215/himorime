//go:build darwin

package envinfo

import "golang.org/x/sys/unix"

func cpuModel() string {
	s, err := unix.Sysctl("machdep.cpu.brand_string")
	if err != nil {
		return ""
	}
	return s
}
