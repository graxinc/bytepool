package bytepool

import (
	"sync"
)

type syncPool struct {
	p sync.Pool
}

// Suitable for similar sized Bytes otherwise pooled
// Bytes can trend to the largest, wasting memory.
// Direct sync.Pool implementation.
func NewSync() Pooler {
	return new(syncPool)
}

func (p *syncPool) Get() *Bytes {
	b := p.p.Get()
	if b == nil {
		return new(Bytes)
	}
	return b.(*Bytes)
}

func (p *syncPool) GetGrown(c int) *Bytes {
	b := p.Get()
	b.B = grow(b.B, c)
	return b
}

func (p *syncPool) Put(b *Bytes) {
	if b == nil {
		return
	}
	b.B = b.B[:0]
	p.p.Put(b)
}
