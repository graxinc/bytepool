package bytepool_test

// originally from https://github.com/valyala/bytebufferpool/blob/master/pool_test.go

import (
	"math/rand"
	"testing"
	"time"

	"github.com/graxinc/bytepool"
)

func TestPoolCalibrate(t *testing.T) {
	t.Parallel()

	p := bytepool.NewDynamic()
	for i := 0; i < 20*42000; i++ { // steps and calibrateCallsThreshold
		n := 1004
		if i%15 == 0 {
			n = rand.Intn(15234) //nolint:gosec
		}
		testGetPut(t, p, n)
	}
}

func TestPoolVariousSizesSerial(t *testing.T) {
	t.Parallel()

	testPoolVariousSizes(t)
}

func TestPoolVariousSizesConcurrent(t *testing.T) {
	t.Parallel()

	concurrency := 5
	ch := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		go func() {
			testPoolVariousSizes(t)
			ch <- struct{}{}
		}()
	}
	for i := 0; i < concurrency; i++ {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout")
		}
	}
}

func testPoolVariousSizes(t *testing.T) {
	p := bytepool.NewDynamic()
	for i := 0; i < 20+1; i++ { // steps
		n := (1 << uint32(i))

		testGetPut(t, p, n)
		testGetPut(t, p, n+1)
		testGetPut(t, p, n-1)

		for j := 0; j < 10; j++ {
			testGetPut(t, p, j+n)
		}
	}
}

func testGetPut(t *testing.T, p bytepool.Pooler, n int) {
	b := p.Get()
	diffFatal(t, 0, len(b.B))
	b.B = allocNBytes(b.B, n)
	p.Put(b)
}

func allocNBytes(dst []byte, n int) []byte {
	diff := n - cap(dst)
	if diff <= 0 {
		return dst[:n]
	}
	return append(dst, make([]byte, diff)...)
}
