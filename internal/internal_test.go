package internal_test

import (
	"fmt"
	"testing"

	"github.com/graxinc/bytepool/internal"
)

func TestGrowMin(t *testing.T) {
	t.Parallel()

	cases := []struct {
		v       []byte
		min     int
		wantCap int
	}{
		{nil, 0, 0},
		{nil, 1, 8},
		{nil, 8, 8},
		{nil, 9, 16},

		{[]byte{1}, 0, 1},
		{[]byte{1}, 1, 1},
		{[]byte{1}, 2, 8},
		{[]byte{1}, 8, 8},
		{[]byte{1}, 9, 16},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("v=%v,min=%v", c.v, c.min), func(t *testing.T) {
			got := internal.GrowMin(c.v, c.min)
			if len(got) != 0 {
				t.Fatal(got)
			}
			if cap(got) != c.wantCap {
				t.Fatal(cap(got))
			}
		})
	}
}

func TestGrowMinMax(t *testing.T) {
	t.Parallel()

	cases := []struct {
		v        []byte
		min, max int
		wantCap  int
	}{
		{nil, 0, 0, 0},
		{nil, 1, 2, 2},
		{nil, 1, 9, 8},
		{nil, 8, 9, 8},
		{nil, 1, 17, 8},

		{[]byte{1}, 0, 0, 0},
		{[]byte{1}, 0, 2, 1},
		{[]byte{1}, 9, 2, 2},
		{[]byte{1}, 9, 15, 15},
		{[]byte{1}, 9, 17, 16},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("v=%v,min=%v,max=%v", c.v, c.min, c.max), func(t *testing.T) {
			got := internal.GrowMinMax(c.v, c.min, c.max)
			if len(got) != 0 {
				t.Fatal(got)
			}
			if cap(got) != c.wantCap {
				t.Fatal(cap(got))
			}
		})
	}
}
