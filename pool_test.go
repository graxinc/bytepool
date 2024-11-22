package bytepool_test

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/grax/bytepool"
)

func TestSizedPooler_concurrentMutation(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, pool bytepool.SizedPooler) {
		runGo := func() {
			rando := rand.New(rand.NewSource(5))
			for range 1000 {
				c1 := 1 + rando.Intn(10)
				c2 := rando.Intn(c1)

				b := pool.GetGrown(c1)

				b.B = b.B[:c1]
				b.B[c2] = byte(rando.Intn(255))

				s1 := string(b.B)
				time.Sleep(time.Millisecond) // time for concurrent mutation
				s2 := string(b.B)

				if s1 != s2 {
					t.Error("concurrent modification")
				}

				pool.Put(b)
			}
		}

		var wait sync.WaitGroup
		for range 10 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				runGo()
			}()
		}
		wait.Wait()
	}
	t.Run("sync", func(t *testing.T) {
		run(t, bytepool.NewSync())
	})
	t.Run("dynamic", func(t *testing.T) {
		run(t, bytepool.NewDynamic())
	})
	t.Run("bucket", func(t *testing.T) {
		run(t, bytepool.NewBucket(1, 20))
	})
}

func TestSizedPooler_lenAndCap(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, pool bytepool.SizedPooler) {
		rando := rand.New(rand.NewSource(5))
		for range 1000 {
			c := 1 + rando.Intn(10)

			b := pool.GetGrown(c)
			diffFatal(t, 0, len(b.B))
			diffFatal(t, true, cap(b.B) >= c)

			b.B = b.B[:c/2]

			pool.Put(b)
		}
	}
	t.Run("sync", func(t *testing.T) {
		run(t, bytepool.NewSync())
	})
	t.Run("dynamic", func(t *testing.T) {
		run(t, bytepool.NewDynamic())
	})
	t.Run("bucket", func(t *testing.T) {
		run(t, bytepool.NewBucket(1, 20))
	})
}

func TestSizedPooler_nilPut(t *testing.T) {
	t.Parallel()

	run := func(_ *testing.T, pool bytepool.SizedPooler) {
		pool.Put(nil)
	}
	t.Run("sync", func(t *testing.T) {
		run(t, bytepool.NewSync())
	})
	t.Run("dynamic", func(t *testing.T) {
		run(t, bytepool.NewDynamic())
	})
	t.Run("bucket", func(t *testing.T) {
		run(t, bytepool.NewBucket(1, 20))
	})
}

func BenchmarkSizedPooler(b *testing.B) {
	run := func(b *testing.B, pool bytepool.SizedPooler, doPut bool) {
		b.RunParallel(func(p *testing.PB) {
			rando := rand.New(rand.NewSource(5)) //nolint:gosec
			for p.Next() {
				c := 2 + rando.Intn(2)
				b := pool.GetGrown(c)
				b.B = b.B[:c]
				b.B[1] = 5
				if doPut {
					pool.Put(b)
				}
			}
		})
	}
	for _, doPut := range []bool{false, true} {
		b.Run(fmt.Sprintf("put=%v", doPut), func(b *testing.B) {
			b.Run("dynamic", func(b *testing.B) {
				run(b, bytepool.NewDynamic(), doPut)
			})
			b.Run("sync", func(b *testing.B) {
				run(b, bytepool.NewSync(), doPut)
			})
			b.Run("bucket", func(b *testing.B) {
				run(b, bytepool.NewBucket(1, 5), doPut)
			})
		})
	}
}

func diffFatal(t testing.TB, want, got any, opts ...cmp.Option) {
	t.Helper()
	if d := cmp.Diff(want, got, opts...); d != "" {
		t.Fatalf("(-want +got):\n%v", d)
	}
}
