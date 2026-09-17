//go:build !windows && !darwin && !ios

package proc

// maxRSSUnit is the size of one ru_maxrss unit in bytes: Linux and the BSDs
// report kilobytes (1024 bytes).
const maxRSSUnit = 1024
