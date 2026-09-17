//go:build unix

package proc

import (
	"runtime/debug"
	"runtime/metrics"
)

// spawnerGCEvery is how much the spawner may allocate before it collects
// garbage. Automatic collection is off in the spawner, so no collection runs
// and competes for a CPU while a command is measured; the spawner collects
// between commands instead, and returns the freed memory to the operating
// system at once. A plain collection keeps freed pages resident, and the
// spawner's RSS, the floor of every later command, then grew by about 1MiB
// every thousand runs.
const spawnerGCEvery = 256 << 10

type heapGrowth struct {
	sample []metrics.Sample
	last   uint64
}

func newHeapGrowth() *heapGrowth {
	h := &heapGrowth{sample: []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}}}
	h.last = h.allocated()
	return h
}

func (h *heapGrowth) allocated() uint64 {
	metrics.Read(h.sample)
	if h.sample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return h.sample[0].Value.Uint64()
}

// collect runs a collection and releases the freed memory once the spawner has
// allocated spawnerGCEvery since the last one.
func (h *heapGrowth) collect() {
	if h.allocated()-h.last < spawnerGCEvery {
		return
	}
	debug.FreeOSMemory()
	h.last = h.allocated()
}
