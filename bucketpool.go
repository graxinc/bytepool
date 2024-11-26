package bytepool

// originally from https://github.com/vitessio/vitess/blob/main/go/bucketpool/bucketpool.go

import (
	"math/bits"
	"sync"
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

type bucketPool struct {
	minSize int
	maxSize int
	pools   []*sizedPool
}

// Suitable for variable sized Bytes if max bounds can be chosen.
// Uses buckets of sizes that increase with the power of two.
// Puts over maxSize will be allocated directly.
func NewBucket(minSize, maxSize int) SizedPooler {
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
	return &bucketPool{
		minSize: minSize,
		maxSize: maxSize,
		pools:   pools,
	}
}

func (p *bucketPool) findPool(size int) *sizedPool {
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

func (p *bucketPool) GetGrown(size int) *Bytes {
	sp := p.findPool(size)
	if sp == nil {
		return makeSizedBytes(size)
	}
	return sp.pool.Get().(*Bytes)
}

func (p *bucketPool) Put(b *Bytes) {
	if b == nil {
		return
	}
	sp := p.findPool(cap(b.B))
	if sp == nil {
		return
	}
	b.B = b.B[:0]
	sp.pool.Put(b)
}

func makeSizedBytes(size int) *Bytes {
	return &Bytes{
		B: make([]byte, 0, size),
	}
}
