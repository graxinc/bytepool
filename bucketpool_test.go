package bytepool_test

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/graxinc/bytepool"

	"github.com/google/go-cmp/cmp"
)

func TestBucket_stats(t *testing.T) {
	t.Parallel()

	t.Run("check results", func(t *testing.T) {
		var lastDiff string
		for range 100 { // can have a buf dropped sometimes
			pool := bytepool.NewBucket(2, 9)
			for j := range 12 {
				buf := pool.GetFilled(j)
				if j == 11 { // putting after appending more
					buf.B = append(buf.B, 1, 2, 3)
				}
				pool.Put(buf)
			}
			got := pool.Stats()
			want := bytepool.BucketPoolStats{
				Buckets: []bytepool.BucketStats{
					{Size: 2, Hits: 2, Misses: 1},
					{Size: 4, Hits: 1, Misses: 1},
					{Size: 8, Hits: 3, Misses: 1},
					{Size: 9, Misses: 1},
				},
				MinSize:  2,
				MaxSize:  9,
				Sizes:    4,
				Hits:     6,
				Misses:   4,
				Overs:    4,
				GetOvers: []int{10, 11},
				PutOvers: []int{10, 24},
			}
			lastDiff = cmp.Diff(want, got)
			if lastDiff == "" {
				return
			}
		}
		t.Fatal(lastDiff)
	})
	t.Run("check for race", func(t *testing.T) {
		pool := bytepool.NewBucket(2, 9)

		do := func() {
			for j := range 12 {
				buf := pool.GetFilled(j)
				if j == 11 { // putting after appending more
					buf.B = append(buf.B, 1, 2, 3)
				}
				pool.Put(buf)
			}
			pool.Stats() // check for race
		}

		for range 10 {
			var wg sync.WaitGroup
			for range 10 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					do()
				}()
			}
			wg.Wait()
		}
	})
}

func TestBucket_getChoice(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fills     []int
		chooseInc int
		want      bytepool.BucketPoolerStats
	}{
		{
			fills:     []int{8, 7, 3, 4, 6},
			chooseInc: 2,
			want: bytepool.BucketPoolerStats{
				Bins: []bytepool.BinStats{
					{Size: 2, Misses: 1},
					{Size: 4, Hits: 1, Misses: 1},
					{Size: 8, Hits: 2},
				},
				DefaultSize: 8,
				Hits:        3,
				Misses:      2,
			},
		},
		{
			fills:     []int{1, 2, 3, 3, 4, 5, 6, 7, 8, 3, 7, 6, 5, 4, 3, 2, 1},
			chooseInc: 3,
			want: bytepool.BucketPoolerStats{
				Bins: []bytepool.BinStats{
					{Size: 2, Puts: 2, Hits: 2, Misses: 1},
					{Size: 4, Puts: 1, Hits: 3, Misses: 2},
					{Size: 8, Puts: 1, Hits: 9},
				},
				DefaultSize: 4,
				Hits:        14,
				Misses:      3,
			},
		},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			sizes := bytepool.Pow2Sizes(2, 8)
			var lastDiff string
			for range 100 { // can have a buf dropped sometimes
				pooler := bytepool.NewBucketFull(sizes).Pooler(bytepool.BucketPoolerOptions{ChooseInc: c.chooseInc})

				for _, f := range c.fills {
					b := pooler.Get()
					fillBytes(b, f)
					pooler.Put(b)
				}

				lastDiff = cmp.Diff(c.want, pooler.Stats())
				if lastDiff == "" {
					return
				}
			}
			t.Fatal(lastDiff)
		})
	}
}

func TestBucket_getChoice_shared(t *testing.T) {
	t.Parallel()

	sizes := bytepool.ExpoSizes(4, 16, 3)
	t.Log("sizes", sizes)

	var lastDiff string
	for range 100 { // can have a buf dropped sometimes
		pool := bytepool.NewBucketFull(sizes)
		pooler1 := pool.Pooler(bytepool.BucketPoolerOptions{ChooseInc: 1})
		pooler2 := pool.Pooler(bytepool.BucketPoolerOptions{ChooseInc: 1})

		do := func(p *bytepool.BucketPooler, fills ...int) {
			for _, f := range fills {
				b := p.Get()
				fillBytes(b, f)
				p.Put(b)
			}
		}
		do(pooler1, 5, 6)
		do(pooler2, 11, 12)

		got := []bytepool.BucketPoolerStats{pooler1.Stats(), pooler2.Stats()}
		want := []bytepool.BucketPoolerStats{
			{
				Bins: []bytepool.BinStats{
					{Size: 4, Misses: 1},
					{Size: 8, Hits: 1},
				},
				DefaultSize: 8,
				Hits:        1,
				Misses:      1,
			},
			{
				Bins: []bytepool.BinStats{
					{Size: 4, Misses: 1},
					{Size: 16, Hits: 1},
				},
				DefaultSize: 16,
				Hits:        1,
				Misses:      1,
			},
		}

		lastDiff = cmp.Diff(want, got)
		if lastDiff == "" {
			return
		}
	}
	t.Fatal(lastDiff)
}

func TestBucket_getChoice_concurrent(t *testing.T) {
	t.Parallel()

	// center 0.5 for n/2.
	normInt := func(rando *rand.Rand, n int, center float64) int {
		f := rando.NormFloat64()

		// normfloat * stddev + desiredMean
		vf := f*(float64(n)/7) + float64(n)*center
		v := int(math.RoundToEven(vf))
		v = min(n, v)
		v = max(0, v)
		return v
	}

	const maxSize = 1000
	sizes := bytepool.ExpoSizes(8, maxSize, 20)

	run := func(t *testing.T, center float64, wantDefMin, wantDefMax int) {
		t.Parallel()

		pooler := bytepool.NewBucketFull(sizes).Pooler(bytepool.BucketPoolerOptions{ChooseInc: 200})

		runGo := func(id byte, rando *rand.Rand) {
			n := normInt(rando, maxSize, center)

			b := pooler.Get()

			if v := len(b.B); v != 0 {
				t.Error(v)
				return
			}
			if v := cap(b.B); v > maxSize {
				t.Error(v)
				return
			}

			randBytes := bytes.Repeat([]byte{id}, n)

			b.B = append(b.B, randBytes...)

			time.Sleep(time.Microsecond)

			if !bytes.Equal(b.B, randBytes) {
				t.Error("not equal")
				return
			}

			pooler.Put(b)
		}

		var wg sync.WaitGroup
		for i := range byte(5) {
			wg.Add(1)
			go func() {
				defer wg.Done()

				rando := rand.New(rand.NewPCG(uint64(i), 0))

				for range 10_000 {
					runGo(i, rando)
				}
			}()
		}
		wg.Wait()

		s := pooler.Stats()
		t.Logf("stats:\n%+v", s)

		if s.DefaultSize < wantDefMin || s.DefaultSize > wantDefMax {
			t.Fatal(s.DefaultSize)
		}
	}
	t.Run("center=0.3", func(t *testing.T) { run(t, 0.3, 280, 470) })
	t.Run("center=0.5", func(t *testing.T) { run(t, 0.5, 450, 650) })
	t.Run("center=0.7", func(t *testing.T) { run(t, 0.7, 650, 780) })
}

func TestBucket_GetFilled_putLess(t *testing.T) {
	t.Parallel()
	pool := bytepool.NewBucket(4, 16)

	buf := pool.GetGrown(7)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 7, cap(buf.B))
	buf.B = make([]byte, 6)
	pool.Put(buf)

	buf = pool.GetFilled(8)
	diffFatal(t, 8, len(buf.B))
	diffFatal(t, 8, cap(buf.B))
}

func TestBucket_getSmaller(t *testing.T) {
	t.Parallel()
	pool := bytepool.NewBucket(32, 64)

	var newCap, prevCap, bucketCap bool
	timeout := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(timeout) {
			t.Fatal("timeout")
		}

		buf := pool.GetGrown(30)
		diffFatal(t, 0, len(buf.B))
		pool.Put(buf)

		buf = pool.GetGrown(28)
		diffFatal(t, 0, len(buf.B))
		c := cap(buf.B)
		if c == 30 {
			prevCap = true
		}
		if c == 28 {
			newCap = true
		}
		if c == 32 {
			bucketCap = true
		}
		pool.Put(buf)

		if prevCap && newCap && bucketCap {
			return
		}
		if !prevCap && !newCap && !bucketCap {
			t.Fatal(c)
		}
	}
}

func TestBucket_basic(t *testing.T) {
	t.Parallel()
	maxSize := 16384
	pool := bytepool.NewBucket(1024, maxSize)

	buf := pool.GetGrown(64)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 64, cap(buf.B))

	// get from same pool
	buf = pool.GetGrown(128)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 128, cap(buf.B))
	pool.Put(buf)
	buf = pool.GetFilled(123)
	diffFatal(t, 123, len(buf.B))
	if c := cap(buf.B); c != 123 && c != 128 {
		t.Fatal(c)
	}
	pool.Put(buf)

	// get boundary size
	buf = pool.GetGrown(1024)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 1024, cap(buf.B))
	pool.Put(buf)
	buf = pool.GetFilled(1024)
	diffFatal(t, 1024, len(buf.B))
	diffFatal(t, 1024, cap(buf.B))
	pool.Put(buf)

	// get from the middle
	buf = pool.GetGrown(5000)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 5000, cap(buf.B))
	pool.Put(buf)
	buf = pool.GetFilled(5000)
	diffFatal(t, 5000, len(buf.B))
	diffFatal(t, 5000, cap(buf.B))
	pool.Put(buf)

	// check last pool
	buf = pool.GetGrown(16383)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 16383, cap(buf.B))
	pool.Put(buf)
	buf = pool.GetFilled(16383)
	diffFatal(t, 16383, len(buf.B))
	diffFatal(t, 16383, cap(buf.B))
	pool.Put(buf)

	// get over last pool
	buf = pool.GetGrown(16385)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 16385, cap(buf.B))
	pool.Put(buf)
	buf = pool.GetFilled(16385)
	diffFatal(t, 16385, len(buf.B))
	diffFatal(t, 16385, cap(buf.B))
	pool.Put(buf)
}

func TestBucket_twoSizeNotMultiplier(t *testing.T) {
	t.Parallel()
	maxSize := 2000
	pool := bytepool.NewBucket(1024, maxSize)

	buf := pool.GetGrown(64)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 64, cap(buf.B))
	pool.Put(buf)

	buf = pool.GetGrown(2001)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 2001, cap(buf.B))
	pool.Put(buf)
}

func TestBucket_weirdMaxSize(t *testing.T) {
	t.Parallel()
	maxSize := 15000
	pool := bytepool.NewBucket(1024, maxSize)

	buf := pool.GetGrown(14000)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 14000, cap(buf.B))
	pool.Put(buf)

	buf = pool.GetGrown(16383)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 16383, cap(buf.B))
	pool.Put(buf)
}

func TestBucket_LinearSizes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		minSize, maxSize, numBuckets int
		want                         []int
	}{
		{2, 3, 2, []int{2, 3}},
		{2, 4, 2, []int{2, 4}},
		{2, 5, 2, []int{2, 5}},

		{2, 4, 3, []int{2, 3, 4}},
		{2, 4, 4, []int{2, 3, 4}},
		{2, 5, 3, []int{2, 4, 5}},
		{2, 10, 3, []int{2, 6, 10}},
		{2, 11, 3, []int{2, 6, 11}},
		{2, 12, 3, []int{2, 7, 12}},

		{2, 5, 4, []int{2, 3, 4, 5}},
		{2, 6, 4, []int{2, 3, 5, 6}},
		{2, 7, 4, []int{2, 4, 5, 7}},

		{1, 1000, 8, []int{1, 144, 286, 429, 572, 715, 857, 1000}},
		{7, 777, 8, []int{7, 117, 227, 337, 447, 557, 667, 777}},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("min=%v,max=%v,num=%v", c.minSize, c.maxSize, c.numBuckets), func(t *testing.T) {
			sizes := bytepool.LinearSizes(c.minSize, c.maxSize, c.numBuckets)
			diffFatal(t, c.want, sizes)
		})
	}

	t.Run("random", func(t *testing.T) {
		rando := rand.New(rand.NewPCG(0, 0))

		var medPercents []float32
		for range 4000 {
			minSize := 1 + rando.IntN(20)
			maxSize := minSize + 1 + rando.IntN(2000)
			numBuckets := 2 + rando.IntN(100)

			sizes := bytepool.LinearSizes(minSize, maxSize, numBuckets)

			if got := sizes[0]; minSize != got {
				t.Fatal(minSize, maxSize, numBuckets, got)
			}
			if got := sizes[len(sizes)-1]; maxSize != got {
				t.Fatal(minSize, maxSize, numBuckets, got)
			}
			wantBuckets := min(numBuckets, maxSize-minSize+1)
			if got := len(sizes); got != wantBuckets {
				t.Fatal(minSize, maxSize, numBuckets, got)
			}

			med := sizes[len(sizes)/2]
			medPercent := float32(med) / float32(maxSize-minSize)
			medPercents = append(medPercents, medPercent)
		}

		slices.Sort(medPercents)
		med := medPercents[len(medPercents)/2]
		if med < 0.48 || med > 0.52 {
			t.Fatal(med)
		}
	})
}

func TestBucket_ExpoSizes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		minSize, maxSize, numBuckets int
		want                         []int
	}{
		{2, 3, 2, []int{2, 3}},
		{2, 4, 2, []int{2, 4}},
		{2, 5, 2, []int{2, 5}},

		{2, 4, 3, []int{2, 3, 4}},
		{2, 4, 4, []int{2, 3, 4}},
		{2, 5, 3, []int{2, 3, 5}},
		{2, 10, 3, []int{2, 4, 10}},
		{2, 14, 3, []int{2, 5, 14}},
		{2, 15, 3, []int{2, 5, 15}},
		{2, 16, 3, []int{2, 6, 16}},

		{2, 5, 4, []int{2, 3, 4, 5}},
		{2, 6, 4, []int{2, 3, 4, 6}},
		{2, 7, 4, []int{2, 3, 5, 7}},

		{1, 1000, 8, []int{1, 3, 7, 19, 52, 139, 373, 1000}},
		{7, 777, 8, []int{7, 14, 27, 53, 103, 202, 396, 777}},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("min=%v,max=%v,num=%v", c.minSize, c.maxSize, c.numBuckets), func(t *testing.T) {
			sizes := bytepool.ExpoSizes(c.minSize, c.maxSize, c.numBuckets)
			diffFatal(t, c.want, sizes)
		})
	}

	t.Run("random", func(t *testing.T) {
		rando := rand.New(rand.NewPCG(0, 0))

		var medPercents []float32
		for range 4000 {
			minSize := 1 + rando.IntN(20)
			maxSize := minSize + 1 + rando.IntN(2000)
			numBuckets := 2 + rando.IntN(100)

			sizes := bytepool.ExpoSizes(minSize, maxSize, numBuckets)

			if got := sizes[0]; minSize != got {
				t.Fatal(minSize, maxSize, numBuckets, got)
			}
			if got := sizes[len(sizes)-1]; maxSize != got {
				t.Fatal(minSize, maxSize, numBuckets, got)
			}
			if got := len(sizes); got > numBuckets {
				t.Fatal(minSize, maxSize, numBuckets, got)
			}

			med := sizes[len(sizes)/2]
			medPercent := float32(med) / float32(maxSize-minSize)
			medPercents = append(medPercents, medPercent)
		}

		slices.Sort(medPercents)
		med := medPercents[len(medPercents)/2]
		if med < 0.10 || med > 0.13 {
			t.Fatal(med)
		}
	})
}

func TestBucket_fuzz(t *testing.T) {
	t.Parallel()
	rando := rand.New(rand.NewPCG(0, 0))

	const maxTestSize = 16384
	for range 20000 {
		minSize := rando.IntN(maxTestSize)
		if minSize == 0 {
			minSize = 1
		}
		maxSize := minSize + 1 + rando.IntN(maxTestSize-minSize)

		p := bytepool.NewBucket(minSize, maxSize)

		bufSize := rando.IntN(maxTestSize)
		buf := p.GetGrown(bufSize)
		diffFatal(t, 0, len(buf.B))
		p.Put(buf)
	}
}

func BenchmarkBucket_getPut(b *testing.B) {
	const maxSize = 16384
	sizes := bytepool.ExpoSizes(2, maxSize, 30)
	pool := bytepool.NewBucketFull(sizes)
	b.Log("sizes", sizes)
	b.SetParallelism(16)
	b.RunParallel(func(pb *testing.PB) {
		rando := rand.New(rand.NewPCG(0, 0))

		for pb.Next() {
			randomSize := rando.IntN(maxSize)
			data := pool.GetGrown(randomSize)
			pool.Put(data)
		}
	})
}

func BenchmarkBucket_get(b *testing.B) {
	const maxSize = 16384
	sizes := bytepool.ExpoSizes(2, maxSize, 30)
	pool := bytepool.NewBucketFull(sizes)
	b.Log("sizes", len(sizes))
	b.SetParallelism(16)
	b.RunParallel(func(pb *testing.PB) {
		rando := rand.New(rand.NewPCG(0, 0))

		for pb.Next() {
			randomSize := rando.IntN(maxSize)
			data := pool.GetGrown(randomSize)
			_ = data
		}
	})
}

func fillBytes(b *bytepool.Bytes, n int) {
	b.B = append(b.B, bytes.Repeat([]byte{5}, n)...)
}
