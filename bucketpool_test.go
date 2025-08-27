package bytepool_test

// originally from https://github.com/vitessio/vitess/blob/main/go/bucketpool/bucketpool_test.go

import (
	"math/rand"
	"sync"
	"testing"

	"github.com/graxinc/bytepool"
)

func TestBucket_stats(t *testing.T) {
	t.Parallel()

	t.Run("check results", func(t *testing.T) {
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
			Hits:         10,
			Overs:        2,
			LastGetOvers: []int{10, 11},
			LastPutOvers: []int{10, 24},
		}
		diffFatal(t, want, got)
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

func TestBucket_GetFilled_putLess(t *testing.T) {
	t.Parallel()
	pool := bytepool.NewBucket(4, 16)

	buf := pool.GetGrown(7)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 8, cap(buf.B))
	buf.B = make([]byte, 7)
	pool.Put(buf)

	buf = pool.GetFilled(8)
	diffFatal(t, 8, len(buf.B))
	diffFatal(t, 8, cap(buf.B))
}

func TestBucket_basic(t *testing.T) {
	t.Parallel()
	maxSize := 16384
	pool := bytepool.NewBucket(1024, maxSize)

	buf := pool.GetGrown(64)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 1024, cap(buf.B))

	// get from same pool, check that length is right
	buf = pool.GetGrown(128)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 1024, cap(buf.B))
	pool.Put(buf)
	buf = pool.GetFilled(123)
	diffFatal(t, 123, len(buf.B))
	diffFatal(t, 1024, cap(buf.B))
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
	diffFatal(t, 8192, cap(buf.B))
	pool.Put(buf)
	buf = pool.GetFilled(5000)
	diffFatal(t, 5000, len(buf.B))
	diffFatal(t, 8192, cap(buf.B))
	pool.Put(buf)

	// check last pool
	buf = pool.GetGrown(16383)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 16384, cap(buf.B))
	pool.Put(buf)
	buf = pool.GetFilled(16383)
	diffFatal(t, 16383, len(buf.B))
	diffFatal(t, 16384, cap(buf.B))
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

func TestBucket_oneSize(t *testing.T) {
	t.Parallel()
	maxSize := 1024
	pool := bytepool.NewBucket(1024, maxSize)

	buf := pool.GetGrown(64)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 1024, cap(buf.B))
	pool.Put(buf)

	buf = pool.GetGrown(1025)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 1025, cap(buf.B))
	pool.Put(buf)
}

func TestBucket_twoSizeNotMultiplier(t *testing.T) {
	t.Parallel()
	maxSize := 2000
	pool := bytepool.NewBucket(1024, maxSize)

	buf := pool.GetGrown(64)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 1024, cap(buf.B))
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
	diffFatal(t, 15000, cap(buf.B))
	pool.Put(buf)

	buf = pool.GetGrown(16383)
	diffFatal(t, 0, len(buf.B))
	diffFatal(t, 16383, cap(buf.B))
	pool.Put(buf)
}

func TestBucket_fuzz(t *testing.T) {
	t.Parallel()
	rando := rand.New(rand.NewSource(5)) //nolint:gosec

	const maxTestSize = 16384
	for range 20000 {
		minSize := rando.Intn(maxTestSize)
		if minSize == 0 {
			minSize = 1
		}
		maxSize := rando.Intn(maxTestSize-minSize) + minSize

		p := bytepool.NewBucket(minSize, maxSize)

		bufSize := rando.Intn(maxTestSize)
		buf := p.GetGrown(bufSize)
		diffFatal(t, 0, len(buf.B))
		p.Put(buf)
	}
}

func BenchmarkBucket_getPut(b *testing.B) {
	const maxSize = 16384
	pool := bytepool.NewBucket(2, maxSize)
	b.SetParallelism(16)
	b.RunParallel(func(pb *testing.PB) {
		rando := rand.New(rand.NewSource(5)) //nolint:gosec

		for pb.Next() {
			randomSize := rando.Intn(maxSize)
			data := pool.GetGrown(randomSize)
			pool.Put(data)
		}
	})
}

func BenchmarkBucket_get(b *testing.B) {
	const maxSize = 16384
	pool := bytepool.NewBucket(2, maxSize)
	b.SetParallelism(16)
	b.RunParallel(func(pb *testing.PB) {
		rando := rand.New(rand.NewSource(5)) //nolint:gosec

		for pb.Next() {
			randomSize := rando.Intn(maxSize)
			data := pool.GetGrown(randomSize)
			_ = data
		}
	})
}
