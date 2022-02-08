// +build test_fuzz

package jd

import (
	"testing"
)

func FuzzJd(f *testing.F) {
	a := `{"foo":{},"bar":[1,null,3]}`
	b := a
	f.Add(a, b)
	f.Fuzz(func(t *testing.T, aStr, bStr string) {
		// Only valid JSON input.
		a, err := ReadJsonString(aStr)
		if err != nil {
			return
		}
		if a == nil {
			t.Errorf("nil parsed input: %q", aStr)
			return
		}
		b, err := ReadJsonString(bStr)
		if err != nil {
			return
		}
		if b == nil {
			t.Errorf("nil parsed input: %q", bStr)
			return
		}
		// Diff A and B.
		d := a.Diff(b)
		if d == nil {
			t.Errorf("nil diff of a and b")
			return
		}
		diffABStr := d.Render()
		diffAB, err := ReadDiffString(diffABStr)
		if err != nil {
			t.Errorf("error parsing diff string %q: %v", diffABStr, err)
			return
		}
		// Apply diff to A to get B.
		patchedA, err := a.Patch(diffAB)
		if err != nil {
			t.Errorf("applying patch %v to %v should give %v. Got err: %v", diffAB, aStr, bStr, err)
			return
		}
		if !patchedA.Equals(b) {
			t.Errorf("applying patch %v to %v should give %v. Got err: %v", diffAB, aStr, bStr, patchedA)
			return
		}
	})
}
