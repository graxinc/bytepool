package bytepool

import (
	"math"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/graxinc/bytepool"
	"github.com/graxinc/bytepool/internal"
)

type Bytes = bytepool.Bytes

type sizedPool struct {
	size int
	pool sync.Pool

	puts   atomic.Int64 // is continually reset
	hits   atomic.Uint64
	misses atomic.Uint64
}

func newSizedPool(size int) *sizedPool {
	return &sizedPool{
		size: size,
	}
}

func (p *sizedPool) get() *Bytes {
	b, _ := p.pool.Get().(*Bytes)
	if b == nil {
		b = makeSizedBytes(p.size)
		p.misses.Add(1)
	} else {
		p.hits.Add(1)
	}
	return b
}

// b cannot be nil. cap(b) can't be over p.size.
func (p *sizedPool) put(b *Bytes) {
	if cap(b.B) > p.size {
		panic("unexpected cap")
	}
	b.B = b.B[:0]
	p.pool.Put(b)
}

type BucketPool struct {
	pools       []*sizedPool
	chooseInc   int64
	decay       float64
	maxPoolPuts int64
	def         atomic.Pointer[sizedPool]
	puts        atomic.Int64
	overs       atomic.Uint64

	statLock atomic.Bool
	getOvers []int
	putOvers []int
}

// sizes that increase with the power of two.
// minSize must be >= 1 and maxSize > minSize.
func Pow2Sizes(minSize, maxSize int) []int {
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
	return sizes
}

// Distributes sizes linearly over numBuckets.
// minSize must be >= 0, maxSize > minSize, and numBuckets >= 2.
func LinearSizes(minSize, maxSize, numBuckets int) []int {
	if minSize < 0 {
		panic("minSize < 0")
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
	sizes = slices.Compact(sizes)
	return sizes
}

// Distributes sizes exponentially over numBuckets.
// minSize must be >= 1, maxSize > minSize, and numBuckets >= 2.
func ExpoSizes(minSize, maxSize, numBuckets int) []int {
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
	sizes = slices.Compact(sizes)
	return sizes
}

// Deprecated.
func NewBucket(minSize, maxSize int) *BucketPool {
	return NewBucketFull(Pow2Sizes(minSize, maxSize), BucketPoolOptions{})
}

type BucketPoolOptions struct {
	ChooseInc   int     // defaults to 1k puts.
	Decay       float64 // defaults to 0.5 (half previous put count).
	MaxPoolPuts int     // defaults to 100 times ChooseInc.
}

// Suitable for variable sized Bytes if max bounds can be chosen.
// Puts over max size will be allocated directly.
// sizes must not be empty and each must be >= 1. Repeats will be removed.
func NewBucketFull(sizes []int, o BucketPoolOptions) *BucketPool {
	if len(sizes) == 0 {
		panic("empty sizes")
	}
	for _, s := range sizes {
		if s < 1 {
			panic("size < 1")
		}
	}
	if o.ChooseInc <= 0 {
		o.ChooseInc = 1000
	}
	if o.Decay <= 0 {
		o.Decay = 0.5
	}
	if o.MaxPoolPuts <= 0 {
		o.MaxPoolPuts = o.ChooseInc * 100
	}

	sizes = slices.Clone(sizes)
	slices.Sort(sizes)
	sizes = slices.Compact(sizes)

	// since pools and bins are not separate and the ranges in sizes can be non-linear, it might
	// push the default pool up or down. However separating bins out bins to linear can lead to
	// a too big smallest bin for a large exponential size set of pools.

	var pools []*sizedPool
	for _, s := range sizes {
		pools = append(pools, newSizedPool(s))
	}

	p := &BucketPool{
		pools:       pools,
		chooseInc:   int64(o.ChooseInc),
		decay:       o.Decay,
		maxPoolPuts: int64(o.MaxPoolPuts),
	}
	p.def.Store(pools[0])
	return p
}

func (p *BucketPool) GetGrown(c int) *Bytes {
	sp := p.findPool(c)
	if sp == nil {
		p.over(c, false)
		return makeSizedBytes(c)
	}
	b := sp.get()
	b.B = internal.GrowMinMax(b.B, c, sp.size)
	return b
}

func (p *BucketPool) GetFilled(len int) *Bytes {
	sp := p.findPool(len)

	var b *Bytes
	if sp == nil {
		p.over(len, false)
		b = makeSizedBytes(len)
	} else {
		b = sp.get()
		b.B = internal.GrowMinMax(b.B, len, sp.size)
	}
	b.B = b.B[:len]
	return b
}

func (p *BucketPool) Get() *Bytes {
	return p.def.Load().get()
}

func (p *BucketPool) Put(b *Bytes) {
	if b == nil {
		return
	}

	pPut := p.findPool(cap(b.B))
	if pPut == nil {
		p.over(cap(b.B), true)
		return
	}
	pInc := p.findPool(len(b.B))
	if pInc == nil {
		// should always find as len is less/equal to cap.
		panic("bad len")
	}

	pInc.puts.Add(1)
	pPut.put(b)

	inc := p.puts.Add(1)

	var doChoose bool
	if inc < p.chooseInc { // ramp up a bit for the first time.
		doChoose = inc == 1 || inc == 10 || inc == 100
	} else {
		doChoose = inc == p.chooseInc*2
	}
	if !doChoose {
		return
	}
	defer p.puts.Store(p.chooseInc)

	p.def.Store(p.chooseDefPool())

	p.reducePuts()
}

type BucketStats struct {
	Size   int
	Puts   int64
	Hits   uint64
	Misses uint64
}

type BucketPoolStats struct {
	Buckets     []BucketStats // only those with positive Hits/Missses
	MinSize     int
	MaxSize     int
	Sizes       int
	DefaultSize int
	Hits        uint64
	Misses      uint64
	Overs       uint64
	GetOvers    []int
	PutOvers    []int
}

func (p *BucketPool) Stats() BucketPoolStats {
	for p.statLock.Swap(true) { // busy loop until not locked
	}
	defer p.statLock.Store(false)

	ps := BucketPoolStats{
		MinSize:     p.pools[0].size,
		MaxSize:     p.pools[len(p.pools)-1].size,
		Sizes:       len(p.pools),
		DefaultSize: p.def.Load().size,
		Overs:       p.overs.Load(),
		GetOvers:    slices.Clone(p.getOvers),
		PutOvers:    slices.Clone(p.putOvers),
	}
	for _, sp := range p.pools {
		s := BucketStats{
			Size:   sp.size,
			Puts:   sp.puts.Load(),
			Hits:   sp.hits.Load(),
			Misses: sp.misses.Load(),
		}
		if s.Puts <= 0 && s.Hits <= 0 && s.Misses <= 0 {
			continue
		}
		ps.Hits += s.Hits
		ps.Misses += s.Misses
		ps.Buckets = append(ps.Buckets, s)
	}
	return ps
}

func (p *BucketPool) findPool(size int) *sizedPool {
	for _, sp := range p.pools {
		if size <= sp.size {
			return sp
		}
	}
	return nil
}

func (p *BucketPool) chooseDefPool() *sizedPool {
	maxPuts := int64(-1)
	var bestPool *sizedPool

	for _, pool := range p.pools {
		v := pool.puts.Load()
		if v > maxPuts {
			maxPuts = v
			bestPool = pool
		}
	}
	return bestPool
}

func (p *BucketPool) reducePuts() {
	for _, pool := range p.pools {
		for {
			v := pool.puts.Load()
			decayed := math.RoundToEven(float64(v) * p.decay)
			v2 := min(int64(decayed), p.maxPoolPuts)
			if pool.puts.CompareAndSwap(v, v2) {
				break
			}
		}
	}
}

func (p *BucketPool) over(over int, isPut bool) {
	p.overs.Add(1)

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
		p.putOvers = add(p.putOvers, over)
	} else {
		p.getOvers = add(p.getOvers, over)
	}
}

func makeSizedBytes(c int) *Bytes {
	return &Bytes{
		B: make([]byte, 0, c),
	}
}
