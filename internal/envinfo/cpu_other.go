//go:build !linux && !darwin && !windows

package envinfo

func cpuModel() string { return "" }
