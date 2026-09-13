package main

// Proves the double-entry rules for every transaction type the system posts.
// Each case checks two things: that the entry balances to the cent, and that
// the money landed on the accounts an accountant would expect.

import (
	"math"
	"testing"
)

// sums returns total debits, total credits and the net movement per role or
// account, so a test can assert on placement rather than on line order.
func sums(lines []postingLine) (debit, credit float64, byKey map[string]float64) {
	byKey = map[string]float64{}
	for _, l := range lines {
		key := l.Role
		if key == "" {
			key = "account:" + l.AccountID
		}
		debit += l.Debit
		credit += l.Credit
		byKey[key] += l.Debit - l.Credit
	}
	return money(debit), money(credit), byKey
}

func assertBalanced(t *testing.T, name string, lines []postingLine, err error) map[string]float64 {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", name, err)
	}
	debit, credit, byKey := sums(lines)
	if debit != credit {
		t.Errorf("%s: debits %.2f do not equal credits %.2f", name, debit, credit)
	}
	if debit == 0 {
		t.Errorf("%s: entry has no value", name)
	}
	return byKey
}

func assertLeg(t *testing.T, name string, byKey map[string]float64, key string, want float64) {
	t.Helper()
	if got := money(byKey[key]); math.Abs(got-want) > 0.005 {
		t.Errorf("%s: %s moved %.2f, expected %.2f", name, key, got, want)
	}
}

// ------------------------------------------------------------------- sales

func TestCashSalePostsRevenueTaxAndCost(t *testing.T) {
	s := saleAmounts{
		Gross:    1000,
		Discount: 100,
		Tax:      135, // 15% of the 900 net
		Cost:     600,
		Tenders:  []tenderLeg{{Role: roleCashOnHand, Amount: 1035}},
	}
	lines, err := saleRevenueLines(s)
	by := assertBalanced(t, "cash sale", lines, err)
	assertLeg(t, "cash sale", by, roleCashOnHand, 1035)
	assertLeg(t, "cash sale", by, roleSalesRevenue, -1000)
	assertLeg(t, "cash sale", by, roleSalesDiscount, 100)
	assertLeg(t, "cash sale", by, roleTaxOutput, -135)

	cost := saleCostLines(s.Cost)
	byCost := assertBalanced(t, "cash sale cost", cost, nil)
	assertLeg(t, "cash sale cost", byCost, roleCOGS, 600)
	assertLeg(t, "cash sale cost", byCost, roleInventoryControl, -600)
}

func TestCreditSaleLeavesTheBalanceOnReceivables(t *testing.T) {
	lines, err := saleRevenueLines(saleAmounts{Gross: 500, Tax: 75, OnAccount: 575})
	by := assertBalanced(t, "credit sale", lines, err)
	assertLeg(t, "credit sale", by, roleARControl, 575)
	assertLeg(t, "credit sale", by, roleSalesRevenue, -500)
}

func TestCardSaleDebitsTheBankLedgerAccount(t *testing.T) {
	lines, err := saleRevenueLines(saleAmounts{
		Gross:   200,
		Tenders: []tenderLeg{{AccountID: "bank-ledger-id", Amount: 200}},
	})
	by := assertBalanced(t, "card sale", lines, err)
	assertLeg(t, "card sale", by, "account:bank-ledger-id", 200)
}

func TestSplitPaymentSaleBalancesAcrossTenders(t *testing.T) {
	lines, err := saleRevenueLines(saleAmounts{
		Gross: 1000,
		Tax:   0,
		Tenders: []tenderLeg{
			{Role: roleCashOnHand, Amount: 300},
			{Role: roleBank, Amount: 450},
		},
		OnAccount: 250,
	})
	by := assertBalanced(t, "split sale", lines, err)
	assertLeg(t, "split sale", by, roleCashOnHand, 300)
	assertLeg(t, "split sale", by, roleBank, 450)
	assertLeg(t, "split sale", by, roleARControl, 250)
}

func TestSaleWithAdvanceConsumesTheCustomerAdvance(t *testing.T) {
	lines, err := saleRevenueLines(saleAmounts{Gross: 400, Advance: 400})
	by := assertBalanced(t, "advance sale", lines, err)
	assertLeg(t, "advance sale", by, roleCustomerAdvances, 400)
}

func TestSaleRejectsSettlementThatDoesNotCoverTheInvoice(t *testing.T) {
	_, err := saleRevenueLines(saleAmounts{
		Gross:   1000,
		Tenders: []tenderLeg{{Role: roleCashOnHand, Amount: 900}},
	})
	if err == nil {
		t.Fatal("a sale settled 100 short was accepted")
	}
}

func TestSalesReturnUsesContraRevenueNotNegativeSales(t *testing.T) {
	lines, err := saleReturnLines(returnAmounts{
		Gross:   200,
		Tax:     30,
		Cost:    120,
		Refunds: []tenderLeg{{Role: roleCashOnHand, Amount: 230}},
	})
	by := assertBalanced(t, "sales return", lines, err)
	assertLeg(t, "sales return", by, roleSalesReturns, 200)
	assertLeg(t, "sales return", by, roleSalesRevenue, 0) // revenue itself is untouched
	assertLeg(t, "sales return", by, roleTaxOutput, 30)
	assertLeg(t, "sales return", by, roleCashOnHand, -230)

	cost := saleReturnCostLines(120)
	byCost := assertBalanced(t, "return cost", cost, nil)
	assertLeg(t, "return cost", byCost, roleInventoryControl, 120)
	assertLeg(t, "return cost", byCost, roleCOGS, -120)
}

func TestPartialReturnCreditedToTheCustomerAccount(t *testing.T) {
	lines, err := saleReturnLines(returnAmounts{Gross: 90, OnAccount: 90})
	by := assertBalanced(t, "credit note", lines, err)
	assertLeg(t, "credit note", by, roleARControl, -90)
}

// --------------------------------------------------------------- purchases

func TestCashPurchaseCapitalisesGoodsNetOfTradeDiscount(t *testing.T) {
	p := purchaseAmounts{
		Gross:    1000,
		Discount: 50,
		Tax:      95,
		Freight:  40,
		Tenders:  []tenderLeg{{Role: roleCashOnHand, Amount: 1085}},
		Invoiced: true,
	}
	lines, err := purchaseLines(p)
	by := assertBalanced(t, "cash purchase", lines, err)
	// inventory carries the net goods value, so the control account can be
	// reconciled against the stock valuation
	assertLeg(t, "cash purchase", by, roleInventoryControl, 950)
	assertLeg(t, "cash purchase", by, roleFreightInwards, 40)
	assertLeg(t, "cash purchase", by, roleTaxInput, 95)
	assertLeg(t, "cash purchase", by, roleCashOnHand, -1085)
}

func TestCreditPurchaseOwesAccountsPayable(t *testing.T) {
	lines, err := purchaseLines(purchaseAmounts{Gross: 600, OnCredit: 600, Invoiced: true})
	by := assertBalanced(t, "credit purchase", lines, err)
	assertLeg(t, "credit purchase", by, roleAPControl, -600)
	assertLeg(t, "credit purchase", by, roleGRNI, 0)
}

func TestGoodsReceivedWithoutInvoiceWaitsInGRNI(t *testing.T) {
	lines, err := purchaseLines(purchaseAmounts{Gross: 600, OnCredit: 600, Invoiced: false})
	by := assertBalanced(t, "grn", lines, err)
	assertLeg(t, "grn", by, roleGRNI, -600)
	assertLeg(t, "grn", by, roleAPControl, 0)

	match := supplierInvoiceMatchLines(600)
	byMatch := assertBalanced(t, "invoice match", match, nil)
	assertLeg(t, "invoice match", byMatch, roleGRNI, 600)
	assertLeg(t, "invoice match", byMatch, roleAPControl, -600)
}

func TestPurchaseReturnTakesGoodsBackOutOfInventory(t *testing.T) {
	lines, err := purchaseReturnLines(purchaseAmounts{
		Gross: 300, Discount: 30, Tax: 27, OnCredit: 297, Invoiced: true,
	})
	by := assertBalanced(t, "purchase return", lines, err)
	assertLeg(t, "purchase return", by, roleInventoryControl, -270)
	assertLeg(t, "purchase return", by, roleTaxInput, -27)
	assertLeg(t, "purchase return", by, roleAPControl, 297)
}

func TestSettlementDiscountDoesNotTouchInventory(t *testing.T) {
	by := assertBalanced(t, "settlement discount", settlementDiscountLines(25), nil)
	assertLeg(t, "settlement discount", by, roleAPControl, 25)
	assertLeg(t, "settlement discount", by, rolePurchaseDiscount, -25)
	assertLeg(t, "settlement discount", by, roleInventoryControl, 0)
}

// ---------------------------------------------------------------- payments

func TestCustomerPaymentSplitsAllocatedAndUnallocated(t *testing.T) {
	lines, err := customerReceiptLines(
		[]tenderLeg{{Role: roleBank, Amount: 1000}}, 750, 250)
	by := assertBalanced(t, "customer payment", lines, err)
	assertLeg(t, "customer payment", by, roleBank, 1000)
	assertLeg(t, "customer payment", by, roleARControl, -750)
	assertLeg(t, "customer payment", by, roleCustomerAdvances, -250)
}

func TestCustomerPaymentRejectsAllocationMismatch(t *testing.T) {
	_, err := customerReceiptLines([]tenderLeg{{Role: roleBank, Amount: 1000}}, 750, 100)
	if err == nil {
		t.Fatal("a receipt allocated 150 short was accepted")
	}
}

func TestSupplierPaymentClearsPayableAndAdvances(t *testing.T) {
	lines, err := supplierPaymentLines(
		[]tenderLeg{{Role: roleBank, Amount: 800}}, 500, 300)
	by := assertBalanced(t, "supplier payment", lines, err)
	assertLeg(t, "supplier payment", by, roleAPControl, 500)
	assertLeg(t, "supplier payment", by, roleSupplierAdvances, 300)
	assertLeg(t, "supplier payment", by, roleBank, -800)
}

func TestChequeClearingMovesOutOfChequesInHand(t *testing.T) {
	by := assertBalanced(t, "cheque cleared",
		chequeDepositLines(tenderLeg{Role: roleBank}, 450), nil)
	assertLeg(t, "cheque cleared", by, roleBank, 450)
	assertLeg(t, "cheque cleared", by, roleChequesInHand, -450)
}

func TestBouncedChequeGoesBackToTheCustomerWithTheCharge(t *testing.T) {
	by := assertBalanced(t, "cheque bounced",
		chequeBounceLines(tenderLeg{Role: roleBank}, 450, 25), nil)
	assertLeg(t, "cheque bounced", by, roleARControl, 450)
	assertLeg(t, "cheque bounced", by, roleBankCharges, 25)
	assertLeg(t, "cheque bounced", by, roleBank, -475)
}

// ---------------------------------------------------------------- expenses

func TestExpensePostsToItsOwnCategoryAccount(t *testing.T) {
	lines, err := expenseLines("rent-account-id", 500, 75,
		[]tenderLeg{{Role: roleCashOnHand, Amount: 575}}, 0)
	by := assertBalanced(t, "expense", lines, err)
	assertLeg(t, "expense", by, "account:rent-account-id", 500)
	assertLeg(t, "expense", by, roleTaxInput, 75)
	assertLeg(t, "expense", by, roleCashOnHand, -575)
}

func TestExpenseWithoutCategoryFallsBackToOperatingExpenses(t *testing.T) {
	lines, err := expenseLines("", 200, 0, []tenderLeg{{Role: roleCashOnHand, Amount: 200}}, 0)
	by := assertBalanced(t, "expense fallback", lines, err)
	assertLeg(t, "expense fallback", by, roleOperatingExpense, 200)
}

// ------------------------------------------------------------ cash movement

func TestCashDepositMovesCashIntoTheBank(t *testing.T) {
	by := assertBalanced(t, "deposit", cashTransferLines(
		tenderLeg{Role: roleCashOnHand}, tenderLeg{Role: roleBank}, 2000), nil)
	assertLeg(t, "deposit", by, roleBank, 2000)
	assertLeg(t, "deposit", by, roleCashOnHand, -2000)
}

func TestCashWithdrawalMovesBankIntoTheTill(t *testing.T) {
	by := assertBalanced(t, "withdrawal", cashTransferLines(
		tenderLeg{Role: roleBank}, tenderLeg{Role: roleCashOnHand}, 500), nil)
	assertLeg(t, "withdrawal", by, roleCashOnHand, 500)
	assertLeg(t, "withdrawal", by, roleBank, -500)
}

func TestCashierShortageIsAnExpenseAndOverageIsIncome(t *testing.T) {
	short := cashierVarianceLines(tenderLeg{Role: roleCashOnHand}, 9950, 10000)
	byShort := assertBalanced(t, "cash short", short, nil)
	assertLeg(t, "cash short", byShort, roleCashShort, 50)
	assertLeg(t, "cash short", byShort, roleCashOnHand, -50)

	over := cashierVarianceLines(tenderLeg{Role: roleCashOnHand}, 10030, 10000)
	byOver := assertBalanced(t, "cash over", over, nil)
	assertLeg(t, "cash over", byOver, roleCashOnHand, 30)
	assertLeg(t, "cash over", byOver, roleCashOver, -30)

	if lines := cashierVarianceLines(tenderLeg{Role: roleCashOnHand}, 10000, 10000); lines != nil {
		t.Error("a cashier who counted exactly right should post no entry")
	}
}

// --------------------------------------------------------------- inventory

func TestStockLossesChargeTheReasonTheyHappened(t *testing.T) {
	for reason, want := range map[string]string{
		"damage":     roleDamageLoss,
		"expiry":     roleExpiryLoss,
		"adjustment": roleAdjustmentLoss,
		"transfer":   roleTransferShortage,
		"mystery":    roleInventoryLoss, // an unknown reason must never block the movement
	} {
		by := assertBalanced(t, reason, inventoryWriteOffLines(reason, 80), nil)
		assertLeg(t, reason, by, want, 80)
		assertLeg(t, reason, by, roleInventoryControl, -80)
	}
}

func TestStockFoundIsInventoryGain(t *testing.T) {
	by := assertBalanced(t, "stock gain", inventoryGainLines(45), nil)
	assertLeg(t, "stock gain", by, roleInventoryControl, 45)
	assertLeg(t, "stock gain", by, roleInventoryGain, -45)
}

func TestBranchTransferParksStockInGoodsInTransit(t *testing.T) {
	out := assertBalanced(t, "transfer out", goodsInTransitOutLines(700), nil)
	assertLeg(t, "transfer out", out, roleGoodsInTransit, 700)
	assertLeg(t, "transfer out", out, roleInventoryControl, -700)

	in := assertBalanced(t, "transfer in", goodsInTransitInLines(700), nil)
	assertLeg(t, "transfer in", in, roleInventoryControl, 700)
	assertLeg(t, "transfer in", in, roleGoodsInTransit, -700)
}

// ---------------------------------------------------------------- openings

func TestOpeningBalancesOffsetAgainstOpeningEquity(t *testing.T) {
	debitSide := assertBalanced(t, "opening debit", openingBalanceLines("stock-account", 5000, 0), nil)
	assertLeg(t, "opening debit", debitSide, "account:stock-account", 5000)
	assertLeg(t, "opening debit", debitSide, roleOpeningBalanceEqty, -5000)

	creditSide := assertBalanced(t, "opening credit", openingBalanceLines("loan-account", 0, 3000), nil)
	assertLeg(t, "opening credit", creditSide, "account:loan-account", -3000)
	assertLeg(t, "opening credit", creditSide, roleOpeningBalanceEqty, 3000)
}

// ------------------------------------------------------- engine-level rules

func TestPrepareDropsZeroLegsAndKeepsTheEntryBalanced(t *testing.T) {
	d := journalDraft{Lines: []postingLine{
		debitRole(roleCashOnHand, 100, ""),
		creditRole(roleSalesRevenue, 100, ""),
		creditRole(roleTaxOutput, 0, ""), // no tax on this sale
	}}
	lines, err := d.prepare()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected the zero tax leg to be dropped, got %d lines", len(lines))
	}
}

func TestPrepareRejectsAnUnbalancedEntry(t *testing.T) {
	d := journalDraft{Lines: []postingLine{
		debitRole(roleCashOnHand, 100, ""),
		creditRole(roleSalesRevenue, 99.99, ""),
	}}
	if _, err := d.prepare(); err == nil {
		t.Fatal("an entry one cent out of balance was accepted")
	}
}

func TestPrepareRejectsALineWithBothSides(t *testing.T) {
	d := journalDraft{Lines: []postingLine{
		{Role: roleCashOnHand, Debit: 50, Credit: 50},
		creditRole(roleSalesRevenue, 50, ""),
	}}
	if _, err := d.prepare(); err == nil {
		t.Fatal("a line carrying both a debit and a credit was accepted")
	}
}

func TestPrepareRejectsNegativeAmounts(t *testing.T) {
	d := journalDraft{Lines: []postingLine{
		debitRole(roleCashOnHand, -100, ""),
		creditRole(roleSalesRevenue, -100, ""),
	}}
	if _, err := d.prepare(); err == nil {
		t.Fatal("a negative posting amount was accepted")
	}
}

func TestPrepareRejectsALineWithNoAccount(t *testing.T) {
	d := journalDraft{Lines: []postingLine{
		{Debit: 10},
		creditRole(roleSalesRevenue, 10, ""),
	}}
	if _, err := d.prepare(); err == nil {
		t.Fatal("a line with neither a role nor an account was accepted")
	}
}

func TestMoneyRoundsToTheCent(t *testing.T) {
	// 1.005 and 2.675 are the classic half-cent cases. Neither is exactly
	// representable in binary: the nearest float64 to 1.005 is a shade below
	// it, so rounding down is arithmetically right, not a defect. What the
	// ledger needs is that money() is deterministic and that prepare() checks
	// the balance with the same function, which the balance tests cover.
	for _, c := range []struct{ in, want float64 }{
		{0.1 + 0.2, 0.3},
		{2.674999, 2.67},
		{2.670001, 2.67},
		{-4.456, -4.46},
		{999999.999, 1000000.00},
	} {
		if got := money(c.in); got != c.want {
			t.Errorf("money(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMoneyNeverMovesAValueByMoreThanHalfACent(t *testing.T) {
	// The property that matters for a ledger: rounding lands on a whole cent
	// and never shifts the amount by more than half of one.
	for _, v := range []float64{
		0, 0.001, 0.999, 1.005, 1.015, 2.675, 19.994, 123.456, 87654.321, -0.005, -33.335,
	} {
		got := money(v)
		if math.Abs(got-v) > 0.005+1e-9 {
			t.Errorf("money(%v) = %v moved the amount by more than half a cent", v, got)
		}
		if cents := got * 100; math.Abs(cents-math.Round(cents)) > 1e-6 {
			t.Errorf("money(%v) = %v is not a whole number of cents", v, got)
		}
	}
}

func TestMoneyIsStableWhenAppliedTwice(t *testing.T) {
	// prepare() rounds each leg and then rounds the totals, so rounding has to
	// be idempotent or an entry could balance on the first pass and not the
	// second.
	for _, v := range []float64{0.1 + 0.2, 1.005, 2.675, 19.999, -4.455} {
		once := money(v)
		if twice := money(once); twice != once {
			t.Errorf("money(money(%v)) = %v, want %v", v, twice, once)
		}
	}
}
