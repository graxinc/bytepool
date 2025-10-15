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
	v := p.p.Get()
	var b *Bytes
	if v == nil {
		b = &Bytes{pool: p}
	} else {
		b = v.(*Bytes)
	}
	return b
}

func (p *syncPool) GetGrown(c int) *Bytes {
	b := p.Get()
	b.B = Grow(b.B, c)
	return b
}

func (p *syncPool) GetFilled(len int) *Bytes {
	b := p.Get()
	b.B = Grow(b.B, len)[:len]
	return b
}

func (p *syncPool) put(b *Bytes) {
	if b == nil {
		return
	}
	b.B = b.B[:0]
	p.p.Put(b)
}
