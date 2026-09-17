//go:build darwin || ios

package proc

// maxRSSUnit is the size of one ru_maxrss unit in bytes: macOS reports bytes.
const maxRSSUnit = 1
