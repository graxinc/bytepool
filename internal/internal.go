package internal

import "slices"

// Ensures capacity for min total elements, but will clip capacity to max.
// Returned slice has len=0.
func GrowMinMax(s []byte, min, max int) []byte {
	s = s[:0]
	s = slices.Grow(s, min) // NOTE this n arg is additional elements based on len.
	if cap(s) < max {
		return s
	}
	s = s[:0:max]
	return s
}

// Ensures capacity for min total elements.
// Returned slice has len=0.
func GrowMin(s []byte, min int) []byte {
	s = s[:0]
	s = slices.Grow(s, min) // NOTE this n arg is additional elements based on len.
	return s
}
