package bytepool

// originally from https://github.com/valyala/bytebufferpool/blob/master/pool.go

import (
	"sort"
	"sync"
	"sync/atomic"
)

const (
	minBitSize = 6 // 2**6=64 is a CPU cache line size
	steps      = 20

	minSize = 1 << minBitSize

	calibrateCallsThreshold = 42000
	maxPercentile           = 0.95
)

type dynamicPool struct {
	calls       [steps]uint64
	calibrating uint64

	defaultSize uint64
	maxSize     uint64

	pool sync.Pool

	callSizes callSizes // buffered for use in calibrate
}

// Continually tunes the Get allocation size and max Put size. Suitable for variable
// sized Bytes, but at a cost.
func NewDynamic() Pooler {
	return new(dynamicPool)
}

func (p *dynamicPool) Get() *Bytes {
	v := p.pool.Get()
	if v == nil {
		return &Bytes{
			B: make([]byte, 0, atomic.LoadUint64(&p.defaultSize)),
		}
	}
	return v.(*Bytes)
}

func (p *dynamicPool) GetGrown(c int) *Bytes {
	b := p.Get()
	b.B = grow(b.B, c)
	return b
}

func (p *dynamicPool) GetFilled(len int) *Bytes {
	b := p.Get()
	b.B = grow(b.B, len)[:len]
	return b
}

func (p *dynamicPool) Put(b *Bytes) {
	if b == nil {
		return
	}

	idx := index(len(b.B))

	if atomic.AddUint64(&p.calls[idx], 1) > calibrateCallsThreshold {
		p.calibrate()
	}

	maxSize := int(atomic.LoadUint64(&p.maxSize))
	if maxSize == 0 || cap(b.B) <= maxSize {
		b.B = b.B[:0]
		p.pool.Put(b)
	}
}

func (p *dynamicPool) calibrate() {
	if !atomic.CompareAndSwapUint64(&p.calibrating, 0, 1) {
		return
	}

	p.callSizes = p.callSizes[:0]
	var callsSum uint64
	for i := 0; i < steps; i++ {
		calls := atomic.SwapUint64(&p.calls[i], 0)
		callsSum += calls
		p.callSizes = append(p.callSizes, callSize{
			calls: calls,
			size:  minSize << i,
		})
	}
	sort.Sort(p.callSizes)

	defaultSize := p.callSizes[0].size
	maxSize := defaultSize

	maxSum := uint64(float64(callsSum) * maxPercentile)
	callsSum = 0
	for i := 0; i < steps; i++ {
		if callsSum > maxSum {
			break
		}
		callsSum += p.callSizes[i].calls
		size := p.callSizes[i].size
		if size > maxSize {
			maxSize = size
		}
	}

	atomic.StoreUint64(&p.defaultSize, defaultSize)
	atomic.StoreUint64(&p.maxSize, maxSize)

	atomic.StoreUint64(&p.calibrating, 0)
}

type callSize struct {
	calls uint64
	size  uint64
}

type callSizes []callSize

func (ci callSizes) Len() int {
	return len(ci)
}

func (ci callSizes) Less(i, j int) bool {
	return ci[i].calls > ci[j].calls
}

func (ci callSizes) Swap(i, j int) {
	ci[i], ci[j] = ci[j], ci[i]
}

func index(n int) int {
	n--
	n >>= minBitSize
	idx := 0
	for n > 0 {
		n >>= 1
		idx++
	}
	if idx >= steps {
		idx = steps - 1
	}
	return idx
}
