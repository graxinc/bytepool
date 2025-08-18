package bytepool

// using *Bytes vs []byte or *[]byte, as we need to allow mutation
// of the pointed item, but giving the original pointer back to the
// to avoid an extra allocation.

type Bytes struct {
	B []byte
}

type SizedPooler interface {
	// Bytes with zero length and minimum capacity c. If giving back
	// to pool, the original pointer should be Put.
	GetGrown(c int) *Bytes

	// Bytes with length. If giving back to pool, the
	// original pointer should be Put.
	GetFilled(length int) *Bytes

	// Can be nil. Do not use Bytes after Put.
	Put(*Bytes)
}

type Pooler interface {
	// Bytes with zero length. If giving back to pool, the
	// original pointer should be Put.
	Get() *Bytes

	SizedPooler
}

// Similar to slices.Grow, but ensures capacity for n total (not just additional appended) elements.
func grow(s []byte, n int) []byte {
	if n < 0 {
		panic("bytepool: n arg to grow cannot be negative")
	}
	need := n - cap(s)
	if need <= 0 {
		return s
	}
	// slices.Grow says: This expression allocates only once (see test).
	return append(s[:cap(s)], make([]byte, need)...)[:len(s)]
}
