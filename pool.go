package bytepool

// using *Bytes vs []byte or *[]byte, as we need to allow mutation
// of the pointed item, but giving the original pointer back to the
// to avoid an extra allocation.

type Bytes struct {
	B []byte
}

type SizedPooler interface {
	// Bytes with zero length and minimum capacity c.
	GetGrown(c int) *Bytes

	// Can be nil.
	Put(*Bytes)
}

type Pooler interface {
	// Bytes with zero length.
	Get() *Bytes

	SizedPooler
}
