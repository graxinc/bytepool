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
// Returned slice has len=0.
func Grow(s []byte, min int) []byte {
	s = s[:0]

	c := cap(s)
	if min < c {
		return s
	}

	// allocates only once, shown in the tests. Similar to slices.Grow (note it isn't for min total).
	return append(s[:cap(s)], make([]byte, min-c)...)[:0]
}
