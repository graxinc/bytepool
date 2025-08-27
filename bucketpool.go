package bytepool

// originally from https://github.com/vitessio/vitess/blob/main/go/bucketpool/bucketpool.go

import (
	"math/bits"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/graxinc/bytepool/internal"
)

type sizedPool struct {
	size int
	pool sync.Pool
}

func newSizedPool(size int) *sizedPool {
	return &sizedPool{
		size: size,
		pool: sync.Pool{
			New: func() any { return makeSizedBytes(size) },
		},
	}
}

type BucketPool struct {
	minSize int
	maxSize int
	pools   []*sizedPool

	hits         atomic.Uint64
	overs        atomic.Uint64
	statLock     atomic.Bool
	lastGetOvers []int
	lastPutOvers []int
}

// Suitable for variable sized Bytes if max bounds can be chosen.
// Uses buckets of sizes that increase with the power of two.
// Puts over maxSize will be allocated directly.
func NewBucket(minSize, maxSize int) *BucketPool {
	if maxSize < minSize {
		panic("maxSize can't be less than minSize")
	}
	const multiplier = 2
	var pools []*sizedPool
	curSize := minSize
	for curSize < maxSize {
		pools = append(pools, newSizedPool(curSize))
		curSize *= multiplier
	}
	pools = append(pools, newSizedPool(maxSize))
	return &BucketPool{
		minSize: minSize,
		maxSize: maxSize,
		pools:   pools,
	}
}

func (p *BucketPool) findPool(size int) *sizedPool {
	if size > p.maxSize {
		return nil
	}
	div, rem := bits.Div64(0, uint64(size), uint64(p.minSize))
	idx := bits.Len64(div)
	if rem == 0 && div != 0 && (div&(div-1)) == 0 {
		idx = idx - 1
	}
	return p.pools[idx]
}

func (p *BucketPool) GetGrown(c int) *Bytes {
	sp := p.findPool(c)
	if sp == nil {
		p.overs.Add(1)
		p.over(c, false)
		return makeSizedBytes(c)
	}
	p.hits.Add(1)
	b := sp.pool.Get().(*Bytes)
	b.B = internal.GrowMinMax(b.B, c, sp.size)
	return b
}

func (p *BucketPool) GetFilled(len int) *Bytes {
	sp := p.findPool(len)

	var b *Bytes
	if sp == nil {
		p.overs.Add(1)
		p.over(len, false)
		b = makeSizedBytes(len)
	} else {
		p.hits.Add(1)
		b = sp.pool.Get().(*Bytes)
		b.B = internal.GrowMinMax(b.B, len, sp.size)
	}
	b.B = b.B[:len]
	return b
}

func (p *BucketPool) Put(b *Bytes) {
	if b == nil {
		return
	}
	sp := p.findPool(cap(b.B))
	if sp == nil {
		p.over(cap(b.B), true)
		return
	}
	b.B = b.B[:0]
	sp.pool.Put(b)
}

type BucketPoolStats struct {
	Hits         uint64
	Overs        uint64
	LastGetOvers []int
	LastPutOvers []int
}

func (p *BucketPool) Stats() BucketPoolStats {
	for p.statLock.Swap(true) { // busy loop until not locked
	}
	defer p.statLock.Store(false)

	return BucketPoolStats{
		Hits:         p.hits.Load(),
		Overs:        p.overs.Load(),
		LastGetOvers: slices.Clone(p.lastGetOvers),
		LastPutOvers: slices.Clone(p.lastPutOvers),
	}
}

func (p *BucketPool) over(over int, isPut bool) {
	if p.statLock.Swap(true) { //  already locked, skip to reduce contention
		return
	}
	defer p.statLock.Store(false)

	add := func(s []int, v int) []int {
		if len(s) > 10 {
			s = s[1:]
		}
		s = append(s, v)
		return s
	}
	if isPut {
		p.lastPutOvers = add(p.lastPutOvers, over)
	} else {
		p.lastGetOvers = add(p.lastGetOvers, over)
	}
}

func makeSizedBytes(c int) *Bytes {
	return &Bytes{
		B: make([]byte, 0, c),
	}
}
