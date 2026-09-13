package main

import (
	"math"
	"testing"
)

func TestAllocatedDocumentLineAmountIncludesHeaderAdjustments(t *testing.T) {
	// LKR 1,000 of line totals with a net LKR 950 document total.
	if got, want := allocatedDocumentLineAmount(400, 950, 1000), 380.0; math.Abs(got-want) > 0.0001 {
		t.Fatalf("allocatedDocumentLineAmount() = %v, want %v", got, want)
	}
}

func TestAllocatedDocumentLineAmountRejectsInvalidBases(t *testing.T) {
	if got := allocatedDocumentLineAmount(100, 90, 0); got != 0 {
		t.Fatalf("allocatedDocumentLineAmount() = %v, want 0", got)
	}
}
