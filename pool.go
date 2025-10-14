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
	// Ideally whatever length/capacity was created from usage should be left in place.
	Put(*Bytes)
}

type Pooler interface {
	// Bytes with zero length. If giving back to pool, the
	// original pointer should be Put.
	Get() *Bytes

	SizedPooler
}

// Ensures capacity for min total elements.
// Min can be <= 0.
// Returned slice has len=0.
func Grow[T any](s []T, min int) []T {
	s = s[:0]

	c := cap(s)
	if min <= c {
		return s
	}

	// allocates only once, shown in the tests. Similar to slices.Grow (note it isn't for min total).
	return append(s[:cap(s)], make([]T, min-c)...)[:0]
}

// Returns s if cap(s) >= size, otherwise makes a new slice with cap=size.
// New slice does not preserve contents of s.
// Size can be <= 0.
// Returned slice has len=0.
func Sized[T any](s []T, size int) []T {
	if size <= cap(s) {
		return s[:0]
	}
	return make([]T, 0, size)
}
