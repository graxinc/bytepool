package bytepool

// originally from https://github.com/vitessio/vitess/blob/main/go/bucketpool/bucketpool.go

import (
	"math"
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
	pools []*sizedPool

	hits         atomic.Uint64
	overs        atomic.Uint64
	statLock     atomic.Bool
	lastGetOvers []int
	lastPutOvers []int
}

// Suitable for variable sized Bytes if max bounds can be chosen.
// Uses buckets of sizes that increase with the power of two.
// Puts over maxSize will be allocated directly.
// minSize must be >= 1 and maxSize > minSize.
func NewBucket(minSize, maxSize int) *BucketPool {
	if minSize < 1 {
		panic("minSize < 1")
	}
	if maxSize <= minSize {
		panic("maxSize <= minSize")
	}

	var sizes []int

	const multiplier = 2
	for s := minSize; s < maxSize; s *= multiplier {
		sizes = append(sizes, s)
	}
	sizes = append(sizes, maxSize)

	return NewBucketFull(sizes)
}

// Suitable for variable sized Bytes if max bounds can be chosen.
// Distributes bucket sizes linearly over numBuckets.
// Puts over max size will be allocated directly.
// minSize must be >= 1, maxSize > minSize, and numBuckets >= 2.
func NewBucketLinear(minSize, maxSize, numBuckets int) *BucketPool {
	if minSize < 1 {
		panic("minSize < 1")
	}
	if maxSize <= minSize {
		panic("maxSize <= minSize")
	}
	if numBuckets < 2 {
		panic("numBuckets < 2")
	}

	var sizes []int

	inc := float64(maxSize-minSize) / float64(numBuckets-1)

	for i := range numBuckets {
		v := float64(minSize) + float64(i)*inc
		sizes = append(sizes, int(math.RoundToEven(v)))
	}

	return NewBucketFull(sizes)
}

// Suitable for variable sized Bytes if max bounds can be chosen.
// Distributes bucket sizes exponentially over numBuckets.
// Puts over max size will be allocated directly.
// minSize must be >= 1, maxSize > minSize, and numBuckets >= 2.
func NewBucketExpo(minSize, maxSize, numBuckets int) *BucketPool {
	if minSize < 1 {
		panic("minSize < 1")
	}
	if maxSize <= minSize {
		panic("maxSize <= minSize")
	}
	if numBuckets < 2 {
		panic("numBuckets < 2")
	}

	var sizes []int

	// size at i = min * (max/min)^(1/(N-1))
	r := math.Pow(float64(maxSize)/float64(minSize), 1/float64(numBuckets-1))

	for i := range numBuckets {
		v := float64(minSize) * math.Pow(r, float64(i))
		sizes = append(sizes, int(math.RoundToEven(v)))
	}
	return NewBucketFull(sizes)
}

// Suitable for variable sized Bytes if max bounds can be chosen.
// Puts over max size will be allocated directly.
// sizes must be >= 1. Repeats will be removed.
func NewBucketFull(sizes []int) *BucketPool {
	if len(sizes) == 0 {
		panic("empty sizes")
	}
	for _, s := range sizes {
		if s < 1 {
			panic("size < 1")
		}
	}

	sizes = slices.Clone(sizes)
	slices.Sort(sizes)
	sizes = slices.Compact(sizes)

	var pools []*sizedPool
	for _, s := range sizes {
		pools = append(pools, newSizedPool(s))
	}
	return &BucketPool{pools: pools}
}

func (p *BucketPool) findPool(size int) *sizedPool {
	for _, sp := range p.pools {
		if size <= sp.size {
			return sp
		}
	}
	return nil
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

func (p *BucketPool) Buckets() []int {
	var v []int
	for _, p := range p.pools {
		v = append(v, p.size)
	}
	return v
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
