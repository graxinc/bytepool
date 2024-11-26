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

	// Can be nil. Do not use Bytes after Put.
	Put(*Bytes)
}

type Pooler interface {
	// Bytes with zero length. If giving back to pool, the
	// original pointer should be Put.
	Get() *Bytes

	SizedPooler
}
