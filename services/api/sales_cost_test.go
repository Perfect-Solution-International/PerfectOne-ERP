package main

import (
	"math"
	"testing"
)

func TestWeightedInventoryCostUsesActualBatchCosts(t *testing.T) {
	allocations := []saleBatchAllocation{
		{id: "older", quantity: 2, unitCost: 100},
		{id: "newer", quantity: 3, unitCost: 140},
	}
	if got, want := weightedInventoryCost(5, 999, allocations), 124.0; math.Abs(got-want) > 0.0001 {
		t.Fatalf("weightedInventoryCost() = %v, want %v", got, want)
	}
}

func TestWeightedInventoryCostUsesFallbackForNonBatchRemainder(t *testing.T) {
	allocations := []saleBatchAllocation{{id: "batch", quantity: 2, unitCost: 80}}
	if got, want := weightedInventoryCost(5, 110, allocations), 98.0; math.Abs(got-want) > 0.0001 {
		t.Fatalf("weightedInventoryCost() = %v, want %v", got, want)
	}
}

func TestWeightedInventoryCostRoundsToFourDecimals(t *testing.T) {
	allocations := []saleBatchAllocation{{id: "batch", quantity: 1, unitCost: 100}}
	if got, want := weightedInventoryCost(3, 101, allocations), 100.6667; math.Abs(got-want) > 0.00001 {
		t.Fatalf("weightedInventoryCost() = %v, want %v", got, want)
	}
}

func TestProratedSaleReturnPreservesLineAndInvoiceDiscounts(t *testing.T) {
	// Two LKR 100 items: this line received a LKR 20 item discount and the
	// invoice received a further LKR 10 discount across a LKR 180 net subtotal.
	if got, want := proratedSaleReturnAmount(1, 1, 80, 170, 180), 75.5555555556; math.Abs(got-want) > 0.00001 {
		t.Fatalf("proratedSaleReturnAmount() = %v, want %v", got, want)
	}
}

func TestProratedSaleReturnHandlesPartialQuantity(t *testing.T) {
	if got, want := proratedSaleReturnAmount(2, 5, 450, 900, 1000), 162.0; math.Abs(got-want) > 0.00001 {
		t.Fatalf("proratedSaleReturnAmount() = %v, want %v", got, want)
	}
}
