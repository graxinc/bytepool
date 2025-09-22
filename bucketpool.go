package bytepool

import (
	"math"
	"slices"
	"sync"
	"sync/atomic"
)

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

type BucketPool struct {
	pools     []*sizedPool
	overs     atomic.Uint64
	oversLock atomic.Bool
	getOvers  []int
	putOvers  []int
}

// Deprecated.
func NewBucket(minSize, maxSize int) *BucketPool {
	return NewBucketFull(Pow2Sizes(minSize, maxSize))
}

// Suitable for variable sized Bytes if max bounds can be chosen.
// Puts over max size will be allocated directly.
// sizes must not be empty and each must be >= 1. Repeats will be removed.
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

func (p *BucketPool) GetGrown(c int) *Bytes {
	_, sp := p.findPool(c)
	if sp == nil {
		p.over(c, false)
		return makeSizedBytes(c)
	}
	b := sp.get(c)
	return b
}

func (p *BucketPool) GetFilled(length int) *Bytes {
	_, sp := p.findPool(length)

	var b *Bytes
	if sp == nil {
		p.over(length, false)
		b = makeSizedBytes(length)
	} else {
		b = sp.get(length)
	}
	b.B = b.B[:length]
	return b
}

type BucketPoolerOptions struct {
	ChooseInc   int     // defaults to 1k puts.
	Decay       float64 // defaults to 0.5 (half previous put count).
	MaxPoolPuts int     // defaults to 100 times ChooseInc.
	BinChecks   int     // defaults to chosen bin plus 3 ahead. Use 1 to turn off lookahead.
}

func (p *BucketPool) Pooler(o BucketPoolerOptions) *BucketPooler {
	if o.ChooseInc <= 0 {
		o.ChooseInc = 1000
	}
	if o.Decay <= 0 {
		o.Decay = 0.5
	}
	if o.MaxPoolPuts <= 0 {
		o.MaxPoolPuts = o.ChooseInc * 100
	}
	if o.BinChecks <= 0 {
		o.BinChecks = 4
	}
	o.BinChecks = max(1, o.BinChecks)

	// since pools and bins are not separate and the ranges in sizes can be non-linear, it might
	// push the default pool up or down. However separating bins out bins to linear can lead to
	// a too big smallest bin for a large exponential size set of pools.

	var bins []*histoBin
	for range p.pools {
		bins = append(bins, &histoBin{})
	}
	pooler := &BucketPooler{
		pool:        p,
		bins:        bins,
		chooseInc:   int64(o.ChooseInc),
		decay:       o.Decay,
		maxPoolPuts: int64(o.MaxPoolPuts),
		binChecks:   o.BinChecks,
	}
	pooler.puts.Store(-9)
	return pooler
}

func (p *BucketPool) Put(b *Bytes) {
	if b == nil {
		return
	}

	_, pool := p.findPool(cap(b.B))
	if pool == nil {
		p.over(cap(b.B), true)
		return
	}
	pool.put(b)
}

type BucketStats struct {
	Size   int
	Hits   uint64
	Misses uint64
}

type BucketPoolStats struct {
	Buckets  []BucketStats // only those with positive counters.
	MinSize  int
	MaxSize  int
	Sizes    int
	Hits     uint64
	Misses   uint64
	Overs    uint64
	GetOvers []int
	PutOvers []int
}

func (p *BucketPool) Stats() BucketPoolStats {
	for p.oversLock.Swap(true) { // busy loop until not locked
	}
	defer p.oversLock.Store(false)

	ps := BucketPoolStats{
		MinSize:  p.pools[0].size,
		MaxSize:  p.pools[len(p.pools)-1].size,
		Sizes:    len(p.pools),
		Overs:    p.overs.Load(),
		GetOvers: slices.Clone(p.getOvers),
		PutOvers: slices.Clone(p.putOvers),
	}
	for _, sp := range p.pools {
		s := BucketStats{
			Size:   sp.size,
			Hits:   sp.hits.Load(),
			Misses: sp.misses.Load(),
		}
		if s.Hits <= 0 && s.Misses <= 0 {
			continue
		}
		ps.Hits += s.Hits
		ps.Misses += s.Misses
		ps.Buckets = append(ps.Buckets, s)
	}
	return ps
}

// -1/nil when not found.
func (p *BucketPool) findPool(size int) (idx int, _ *sizedPool) {
	for i, sp := range p.pools {
		if size <= sp.size {
			return i, sp
		}
	}
	return -1, nil
}

func (p *BucketPool) over(over int, isPut bool) {
	p.overs.Add(1)

	if p.oversLock.Swap(true) { //  already locked, skip to reduce contention
		return
	}
	defer p.oversLock.Store(false)

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

type histoBin struct {
	puts            atomic.Int64
	hits            atomic.Uint64
	hitsLookahead   atomic.Uint64
	misses          atomic.Uint64
	missesLookahead atomic.Uint64
}

type BucketPooler struct {
	// immutable
	pool        *BucketPool
	chooseInc   int64
	maxPoolPuts int64
	decay       float64
	binChecks   int

	bins   []*histoBin // slice immutable, same length as sizes in pool.
	defIdx atomic.Int64
	puts   atomic.Int64 // starts at -9
}

func (g *BucketPooler) GetGrown(c int) *Bytes {
	return g.pool.GetGrown(c)
}

func (g *BucketPooler) GetFilled(length int) *Bytes {
	return g.pool.GetFilled(length)
}

func (g *BucketPooler) Get() *Bytes {
	defIdx := g.defIdx.Load()

	for i := range g.binChecks {
		idx := defIdx + int64(i)
		if idx >= int64(len(g.bins)) {
			break
		}

		b := g.pool.pools[idx].getNoAlloc(0)
		if b == nil {
			continue
		}
		bin := g.bins[idx]
		if i > 0 {
			bin.hitsLookahead.Add(1)
			g.bins[defIdx].missesLookahead.Add(1)
		}
		bin.hits.Add(1)
		return b
	}

	b := g.pool.pools[defIdx].allocate(0)
	g.bins[defIdx].misses.Add(1)
	return b
}

func (g *BucketPooler) Put(b *Bytes) {
	if b == nil {
		return
	}

	defer g.pool.Put(b) // after len use below

	idx, _ := g.pool.findPool(len(b.B))
	if idx < 0 {
		return
	}

	g.bins[idx].puts.Add(1)

	inc := g.puts.Add(1)

	if inc > 0 {
		if inc != g.chooseInc {
			return
		}
		defer g.puts.Store(0)
	} // else ramp from negative for first times.

	g.chooseDefPool()
	g.reducePuts()
}

type BinStats struct {
	Size            int
	Puts            int64
	Hits            uint64
	Misses          uint64
	HitsLookahead   uint64
	MissesLookahead uint64
}

type BucketPoolerStats struct {
	Bins            []BinStats // only those with positive counters
	DefaultSize     int
	Hits            uint64
	HitsLookahead   uint64
	Misses          uint64
	MissesLookahead uint64
}

func (g *BucketPooler) Stats() BucketPoolerStats {
	ps := BucketPoolerStats{
		DefaultSize: g.pool.pools[g.defIdx.Load()].size,
	}
	for i, bin := range g.bins {
		s := BinStats{
			Size:            g.pool.pools[i].size,
			Puts:            bin.puts.Load(),
			Hits:            bin.hits.Load(),
			Misses:          bin.misses.Load(),
			HitsLookahead:   bin.hitsLookahead.Load(),
			MissesLookahead: bin.missesLookahead.Load(),
		}
		if s.Puts <= 0 && s.Hits <= 0 && s.Misses <= 0 && s.HitsLookahead <= 0 && s.MissesLookahead <= 0 {
			continue
		}
		ps.Hits += s.Hits
		ps.Misses += s.Misses
		ps.HitsLookahead += s.HitsLookahead
		ps.MissesLookahead += s.MissesLookahead
		ps.Bins = append(ps.Bins, s)
	}
	return ps
}

func (g *BucketPooler) chooseDefPool() {
	maxPuts := int64(-1)
	var bestPool int

	for i, bin := range g.bins {
		v := bin.puts.Load()
		if v > maxPuts {
			maxPuts = v
			bestPool = i
		}
	}
	g.defIdx.Store(int64(bestPool))
}

func (g *BucketPooler) reducePuts() {
	for _, bin := range g.bins {
		for {
			v := bin.puts.Load()
			decayed := math.RoundToEven(float64(v) * g.decay)
			v2 := min(int64(decayed), g.maxPoolPuts)
			if bin.puts.CompareAndSwap(v, v2) {
				break
			}
		}
	}
}

type sizedPool struct {
	size int
	pool sync.Pool

	hits   atomic.Uint64
	misses atomic.Uint64
}

func newSizedPool(size int) *sizedPool {
	return &sizedPool{size: size}
}

// returned bytes will have cap >= c if c is positive.
// c cannot be over p.size.
func (p *sizedPool) get(c int) *Bytes {
	b := p.getNoAlloc(c)
	if b != nil {
		return b
	}
	return p.allocate(c)
}

// returns nil if miss.
// c cannot be over p.size.
func (p *sizedPool) getNoAlloc(c int) *Bytes {
	if c > p.size {
		panic("unexpected c")
	}

	b, _ := p.pool.Get().(*Bytes)
	if b == nil {
		return nil
	}
	p.hits.Add(1)
	b.B = Grow(b.B, c)
	return b
}

// returned bytes will have cap >= c if c is positive.
// c cannot be over p.size.
func (p *sizedPool) allocate(c int) *Bytes {
	if c > p.size {
		panic("unexpected c")
	}
	var b *Bytes
	if c <= 0 {
		b = makeSizedBytes(p.size)
	} else {
		b = makeSizedBytes(c)
	}
	p.misses.Add(1)
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

// returned bytes have cap c and zero len.
func makeSizedBytes(c int) *Bytes {
	return &Bytes{
		B: make([]byte, 0, c),
	}
}
