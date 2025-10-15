package bytepool_test

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/graxinc/bytepool"
)

func TestSizedPooler_concurrentMutation(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, pool bytepool.SizedPooler) {
		runGo := func() {
			rando := rand.New(rand.NewPCG(0, 0))
			for range 1000 {
				c1 := 1 + rando.IntN(10)
				c2 := rando.IntN(c1)

				b := pool.GetGrown(c1)

				b.B = b.B[:c1]
				b.B[c2] = byte(rando.IntN(255))

				s1 := string(b.B)
				time.Sleep(time.Millisecond) // time for concurrent mutation
				s2 := string(b.B)

				if s1 != s2 {
					t.Error("concurrent modification")
				}

				b.Release()
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
	t.Run("bucket_pooler", func(t *testing.T) {
		pool := bytepool.NewBucket(1, 20)
		run(t, pool.Pooler(bytepool.BucketPoolerOptions{}))
	})
}

func TestSizedPooler_lenAndCap(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, pool bytepool.SizedPooler) {
		rando := rand.New(rand.NewPCG(0, 0))
		for range 4000 {
			c := 1 + rando.IntN(10)

			var b *bytepool.Bytes
			if rando.IntN(2) == 0 {
				b = pool.GetGrown(c)
				diffFatal(t, 0, len(b.B))
			} else {
				b = pool.GetFilled(c)
				diffFatal(t, c, len(b.B))
			}

			if cap(b.B) < c {
				t.Fatal(cap(b.B), c)
			}
			if c < 8 && cap(b.B) > c*32 {
				t.Fatal(cap(b.B), c)
			}
			if c > 8 && cap(b.B) > c*4 {
				t.Fatal(cap(b.B), c)
			}

			if rando.IntN(5) == 0 {
				b.B = make([]byte, rando.IntN(10))
			} else {
				b.B = b.B[:c/2]
			}

			b.Release()
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
	t.Run("bucket_pooler", func(t *testing.T) {
		pool := bytepool.NewBucket(1, 20)
		run(t, pool.Pooler(bytepool.BucketPoolerOptions{}))
	})
}

func TestBytes_nilRelease(t *testing.T) {
	t.Parallel()

	var b *bytepool.Bytes
	b.Release()
}

func TestGrowAllocs(t *testing.T) {
	do := func(n1, n2 int) {
		buf := make([]byte, n1)
		buf = bytepool.Grow(buf, n2)
		if len(buf) != 0 {
			t.Fatal(buf)
		}
		if cap(buf) < n2 {
			t.Fatal(cap(buf))
		}
	}

	cases := []struct {
		n1, n2 int
		alloc  bool
	}{
		{1, 0, false},
		{1, 1, false},
		{10, 0, false},
		{10, 9, false},
		{10, 10, false},
		{10, 11, true},
		{10, 12, true},
		{10, 16, true},
		{10, 32, true},
		{10, 33, true},
		// n2 > ~40 will allocate more with race detector on, avoiding.
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("n1=%v,n2=%v", c.n1, c.n2), func(t *testing.T) {
			allocs := testing.AllocsPerRun(1000, func() {
				do(c.n1, c.n2)
			})
			want := 1.0 // make only
			if c.alloc {
				want = 2.0 // make and grow
			}
			if allocs != want { // make and grow
				t.Fatal(want, allocs)
			}
		})
	}
}

func TestGrow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		v       []byte
		min     int
		wantCap int
	}{
		{nil, -1, 0},
		{nil, 0, 0},
		{nil, 1, 8},
		{nil, 8, 8},
		{nil, 9, 16},

		{[]byte{1}, -1, 1},
		{[]byte{1}, 0, 1},
		{[]byte{1}, 1, 1},
		{[]byte{1}, 2, 8},
		{[]byte{1}, 8, 8},
		{[]byte{1}, 9, 16},

		{[]byte{1, 2}, 2, 2},
		{[]byte{1, 2}, 3, 8},
		{[]byte{1, 2}, 9, 16},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("v=%v,min=%v", c.v, c.min), func(t *testing.T) {
			got := bytepool.Grow(c.v, c.min)
			if len(got) != 0 {
				t.Fatal(got)
			}
			if cap(got) != c.wantCap {
				t.Fatal(cap(got), c.wantCap)
			}
		})
	}

	t.Run("preserves", func(t *testing.T) {
		v := []byte{1, 2}
		got := bytepool.Grow(v, 3)
		if len(got) != 0 {
			t.Fatal(got)
		}
		if cap(got) < 3 {
			t.Fatal(cap(got))
		}
		want := []byte{1, 2}
		diffFatal(t, want, got[:2])
	})
}

func TestSized(t *testing.T) {
	t.Parallel()

	cases := []struct {
		v       []byte
		size    int
		wantCap int
	}{
		{nil, -1, 0},
		{nil, 0, 0},
		{nil, 1, 1},
		{nil, 8, 8},
		{nil, 9, 9},

		{[]byte{1}, -1, 1},
		{[]byte{1}, 0, 1},
		{[]byte{1}, 1, 1},
		{[]byte{1}, 2, 2},
		{[]byte{1}, 8, 8},
		{[]byte{1}, 9, 9},

		{[]byte{1, 2}, 2, 2},
		{[]byte{1, 2}, 3, 3},
		{[]byte{1, 2}, 9, 9},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("v=%v,size=%v", c.v, c.size), func(t *testing.T) {
			got := bytepool.Sized(c.v, c.size)
			if len(got) != 0 {
				t.Fatal(got)
			}
			if cap(got) != c.wantCap {
				t.Fatal(cap(got), c.wantCap)
			}
		})
	}

	t.Run("discards", func(t *testing.T) {
		v := []byte{1, 2}
		got := bytepool.Sized(v, 3)
		if len(got) != 0 {
			t.Fatal(got)
		}
		if cap(got) < 3 {
			t.Fatal(cap(got))
		}
		want := []byte{0, 0}
		diffFatal(t, want, got[:2])
	})
}

func BenchmarkSizedPooler(b *testing.B) {
	run := func(b *testing.B, pool bytepool.SizedPooler, doRelease bool) {
		b.RunParallel(func(p *testing.PB) {
			rando := rand.New(rand.NewPCG(0, 0))
			for p.Next() {
				c := 2 + rando.IntN(2)
				b := pool.GetGrown(c)
				b.B = b.B[:c]
				b.B[1] = 5
				if doRelease {
					b.Release()
				}
			}
		})
	}
	for _, doRelease := range []bool{false, true} {
		b.Run(fmt.Sprintf("release=%v", doRelease), func(b *testing.B) {
			b.Run("dynamic", func(b *testing.B) {
				run(b, bytepool.NewDynamic(), doRelease)
			})
			b.Run("sync", func(b *testing.B) {
				run(b, bytepool.NewSync(), doRelease)
			})
			b.Run("bucket", func(b *testing.B) {
				run(b, bytepool.NewBucket(1, 5), doRelease)
			})
			b.Run("bucket_pooler", func(b *testing.B) {
				pool := bytepool.NewBucket(1, 5)
				run(b, pool.Pooler(bytepool.BucketPoolerOptions{}), doRelease)
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
