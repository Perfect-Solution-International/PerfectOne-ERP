package main

// Double-entry posting rules.
//
// This file is the single place that says what each business document does to
// the ledger. Callers compute the amounts and hand them over; the rule builds
// the balanced set of legs. Nothing else in the codebase should be writing
// journal lines by hand.
//
// Conventions used throughout:
//
//	gross    the value of goods before any discount or tax
//	discount the reduction given on gross
//	net      gross - discount, the taxable value
//	tax      tax charged on net
//	total    net + tax, the amount actually settled
//	cost     the inventory cost of the goods, always posted separately from
//	         the revenue side so that COGS can be restated without touching
//	         the customer's invoice

import (
	"fmt"
	"math"
	"strings"
)

// tenderLeg is one way money moved: a specific cash drawer or bank ledger
// account when the caller knows it, otherwise the generic cash or bank role.
type tenderLeg struct {
	AccountID string
	Role      string
	Amount    float64
	Memo      string
}

func (t tenderLeg) debit() postingLine {
	if t.AccountID != "" {
		return debitAccount(t.AccountID, t.Amount, t.Memo)
	}
	return debitRole(t.roleOrCash(), t.Amount, t.Memo)
}

func (t tenderLeg) credit() postingLine {
	if t.AccountID != "" {
		return creditAccount(t.AccountID, t.Amount, t.Memo)
	}
	return creditRole(t.roleOrCash(), t.Amount, t.Memo)
}

func (t tenderLeg) roleOrCash() string {
	if t.Role != "" {
		return t.Role
	}
	return roleCashOnHand
}

// tenderRoleForMethod maps a payment method captured at the till onto the
// account the money lands in. A cheque is deliberately not bank: it sits in
// cheques-in-hand until it clears, which is what makes the bank reconciliation
// work.
func tenderRoleForMethod(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "bank", "card", "online", "transfer", "wallet":
		return roleBank
	case "cheque", "check":
		return roleChequesInHand
	default:
		return roleCashOnHand
	}
}

// ------------------------------------------------------------------- sales

// saleAmounts describes one invoice, however it was paid.
type saleAmounts struct {
	Gross     float64
	Discount  float64
	Tax       float64
	Cost      float64     // inventory cost of the goods sold
	Tenders   []tenderLeg // cash, card, bank, cheque... may be empty for pure credit
	OnAccount float64     // the part left owing by the customer
	Advance   float64     // customer advance consumed to settle the invoice
}

// net is the taxable value of the invoice.
func (s saleAmounts) net() float64 { return money(s.Gross - s.Discount) }

// total is what the customer owes in all.
func (s saleAmounts) total() float64 { return money(s.net() + s.Tax) }

// saleRevenueLines builds the revenue side of a sale. Cash, credit, card and
// split-tender sales all use this; the difference is only which tenders are
// present and how much is left on account.
//
//	Dr  tenders (cash / bank / card)      amount settled now
//	Dr  customer advances                 advance applied
//	Dr  accounts receivable               amount left on account
//	Dr  sales discounts                   discount given
//	    Cr  sales revenue                 gross
//	    Cr  output tax                    tax charged
func saleRevenueLines(s saleAmounts) ([]postingLine, error) {
	lines := []postingLine{}
	settled := 0.0
	for _, t := range s.Tenders {
		if money(t.Amount) == 0 {
			continue
		}
		lines = append(lines, t.debit())
		settled += t.Amount
	}
	if money(s.Advance) != 0 {
		lines = append(lines, debitRole(roleCustomerAdvances, s.Advance, "advance applied"))
		settled += s.Advance
	}
	if money(s.OnAccount) != 0 {
		lines = append(lines, debitRole(roleARControl, s.OnAccount, "on account"))
		settled += s.OnAccount
	}
	if money(settled) != s.total() {
		return nil, fmt.Errorf(
			"sale settlement %.2f does not match invoice total %.2f", settled, s.total())
	}
	lines = append(lines,
		debitRole(roleSalesDiscount, s.Discount, "discount"),
		creditRole(roleSalesRevenue, s.Gross, "sales"),
		creditRole(roleTaxOutput, s.Tax, "output tax"),
	)
	return lines, nil
}

// saleCostLines moves the goods out of inventory and into cost of sales.
//
//	Dr  cost of goods sold
//	    Cr  inventory
func saleCostLines(cost float64) []postingLine {
	return []postingLine{
		debitRole(roleCOGS, cost, "cost of goods sold"),
		creditRole(roleInventoryControl, cost, "goods delivered"),
	}
}

// returnAmounts describes a full or partial sales return.
type returnAmounts struct {
	Gross     float64
	Discount  float64 // the share of the original discount being given back
	Tax       float64
	Cost      float64     // cost of the goods coming back into stock
	Refunds   []tenderLeg // cash or bank actually paid out
	OnAccount float64     // credited against the customer's balance instead
	Advance   float64     // held as a customer advance for later use
}

func (r returnAmounts) net() float64   { return money(r.Gross - r.Discount) }
func (r returnAmounts) total() float64 { return money(r.net() + r.Tax) }

// saleReturnLines is the mirror of a sale, but it credits a contra-revenue
// account rather than debiting sales, so gross revenue for the period stays
// visible and returns can be reported separately.
//
//	Dr  sales returns                     gross returned
//	Dr  output tax                        tax given back
//	    Cr  refund tenders                cash or bank paid out
//	    Cr  accounts receivable           credited to the customer
//	    Cr  customer advances             held for later
//	    Cr  sales discounts               discount clawed back
func saleReturnLines(r returnAmounts) ([]postingLine, error) {
	lines := []postingLine{
		debitRole(roleSalesReturns, r.Gross, "sales return"),
		debitRole(roleTaxOutput, r.Tax, "output tax reversed"),
	}
	settled := 0.0
	for _, t := range r.Refunds {
		if money(t.Amount) == 0 {
			continue
		}
		lines = append(lines, t.credit())
		settled += t.Amount
	}
	if money(r.OnAccount) != 0 {
		lines = append(lines, creditRole(roleARControl, r.OnAccount, "credited to account"))
		settled += r.OnAccount
	}
	if money(r.Advance) != 0 {
		lines = append(lines, creditRole(roleCustomerAdvances, r.Advance, "held as advance"))
		settled += r.Advance
	}
	if money(settled) != r.total() {
		return nil, fmt.Errorf(
			"return settlement %.2f does not match return total %.2f", settled, r.total())
	}
	lines = append(lines, creditRole(roleSalesDiscount, r.Discount, "discount reversed"))
	return lines, nil
}

// saleReturnCostLines puts the goods back into stock at the cost they left at.
//
//	Dr  inventory
//	    Cr  cost of goods sold
func saleReturnCostLines(cost float64) []postingLine {
	return []postingLine{
		debitRole(roleInventoryControl, cost, "goods returned to stock"),
		creditRole(roleCOGS, cost, "cost of sales reversed"),
	}
}

// --------------------------------------------------------------- purchases

// purchaseAmounts describes a goods receipt or supplier invoice.
type purchaseAmounts struct {
	Gross    float64
	Discount float64     // trade discount received
	Tax      float64     // recoverable input tax
	Freight  float64     // carriage charged on the same document
	Tenders  []tenderLeg // paid immediately
	OnCredit float64     // left owing to the supplier
	Advance  float64     // supplier advance consumed
	// Invoiced is false for a goods receipt with no supplier invoice yet, in
	// which case the payable sits in goods-received-not-invoiced instead of
	// accounts payable.
	Invoiced bool
}

func (p purchaseAmounts) net() float64 { return money(p.Gross - p.Discount) }
func (p purchaseAmounts) total() float64 {
	return money(p.net() + p.Tax + p.Freight)
}

// purchaseLines capitalises goods into inventory and records what is owed.
//
//	Dr  inventory                         net goods value
//	Dr  freight inwards                   carriage
//	Dr  input tax                         recoverable tax
//	    Cr  purchase discounts received   discount
//	    Cr  tenders                       paid now
//	    Cr  supplier advances             advance used up
//	    Cr  accounts payable / GRNI       left owing
func purchaseLines(p purchaseAmounts) ([]postingLine, error) {
	// A trade discount reduces what the goods cost, so inventory is debited
	// net. Booking it to a separate discount account instead would leave the
	// inventory control account standing above the stock valuation by the
	// discount, and the two would never reconcile. Settlement discounts taken
	// later, which do not change cost, go through settlementDiscountLines.
	lines := []postingLine{
		debitRole(roleInventoryControl, p.net(), "goods received"),
		debitRole(roleFreightInwards, p.Freight, "freight"),
		debitRole(roleTaxInput, p.Tax, "input tax"),
	}
	settled := 0.0
	for _, t := range p.Tenders {
		if money(t.Amount) == 0 {
			continue
		}
		lines = append(lines, t.credit())
		settled += t.Amount
	}
	if money(p.Advance) != 0 {
		lines = append(lines, creditRole(roleSupplierAdvances, p.Advance, "advance applied"))
		settled += p.Advance
	}
	if money(p.OnCredit) != 0 {
		payable := roleAPControl
		memo := "on credit"
		if !p.Invoiced {
			payable = roleGRNI
			memo = "awaiting supplier invoice"
		}
		lines = append(lines, creditRole(payable, p.OnCredit, memo))
		settled += p.OnCredit
	}
	if money(settled) != p.total() {
		return nil, fmt.Errorf(
			"purchase settlement %.2f does not match document total %.2f", settled, p.total())
	}
	return lines, nil
}

// supplierInvoiceMatchLines clears a goods receipt out of GRNI once the
// supplier's invoice arrives.
//
//	Dr  goods received not invoiced
//	    Cr  accounts payable
func supplierInvoiceMatchLines(amount float64) []postingLine {
	return []postingLine{
		debitRole(roleGRNI, amount, "goods receipt matched"),
		creditRole(roleAPControl, amount, "supplier invoice"),
	}
}

// purchaseReturnLines sends goods back to the supplier.
//
//	Dr  accounts payable / refund tenders
//	    Cr  inventory
//	    Cr  input tax
func purchaseReturnLines(p purchaseAmounts) ([]postingLine, error) {
	lines := []postingLine{}
	settled := 0.0
	for _, t := range p.Tenders {
		if money(t.Amount) == 0 {
			continue
		}
		lines = append(lines, t.debit())
		settled += t.Amount
	}
	if money(p.OnCredit) != 0 {
		payable := roleAPControl
		if !p.Invoiced {
			payable = roleGRNI
		}
		lines = append(lines, debitRole(payable, p.OnCredit, "supplier credit note"))
		settled += p.OnCredit
	}
	total := money(p.net() + p.Tax)
	if money(settled) != total {
		return nil, fmt.Errorf(
			"purchase return settlement %.2f does not match return total %.2f", settled, total)
	}
	lines = append(lines,
		creditRole(roleInventoryControl, p.net(), "goods returned"),
		creditRole(roleTaxInput, p.Tax, "input tax reversed"),
	)
	return lines, nil
}

// settlementDiscountLines records a discount the supplier granted for paying
// early. Unlike a trade discount it does not change what the goods cost, so it
// clears the payable against income rather than against inventory.
//
//	Dr  accounts payable
//	    Cr  purchase discounts received
func settlementDiscountLines(amount float64) []postingLine {
	return []postingLine{
		debitRole(roleAPControl, amount, "settlement discount"),
		creditRole(rolePurchaseDiscount, amount, "discount received"),
	}
}

// ---------------------------------------------------------------- payments

// customerReceiptLines records money coming in from a customer. Whatever is
// not matched to invoices is held as an advance.
//
//	Dr  cash / bank
//	    Cr  accounts receivable      allocated to invoices
//	    Cr  customer advances        left unallocated
func customerReceiptLines(received []tenderLeg, allocated, unallocated float64) ([]postingLine, error) {
	lines := []postingLine{}
	total := 0.0
	for _, t := range received {
		if money(t.Amount) == 0 {
			continue
		}
		lines = append(lines, t.debit())
		total += t.Amount
	}
	if money(total) != money(allocated+unallocated) {
		return nil, fmt.Errorf(
			"receipt of %.2f does not match allocation of %.2f", total, money(allocated+unallocated))
	}
	lines = append(lines,
		creditRole(roleARControl, allocated, "invoice settlement"),
		creditRole(roleCustomerAdvances, unallocated, "unallocated receipt"),
	)
	return lines, nil
}

// supplierPaymentLines records money going out to a supplier.
//
//	Dr  accounts payable         allocated to invoices
//	Dr  supplier advances        paid ahead of an invoice
//	    Cr  cash / bank
func supplierPaymentLines(paid []tenderLeg, allocated, prepaid float64) ([]postingLine, error) {
	lines := []postingLine{
		debitRole(roleAPControl, allocated, "invoice settlement"),
		debitRole(roleSupplierAdvances, prepaid, "advance to supplier"),
	}
	total := 0.0
	for _, t := range paid {
		if money(t.Amount) == 0 {
			continue
		}
		lines = append(lines, t.credit())
		total += t.Amount
	}
	if money(total) != money(allocated+prepaid) {
		return nil, fmt.Errorf(
			"payment of %.2f does not match allocation of %.2f", total, money(allocated+prepaid))
	}
	return lines, nil
}

// chequeDepositLines moves a customer cheque out of cheques-in-hand once the
// bank clears it.
//
//	Dr  bank
//	    Cr  cheques in hand
func chequeDepositLines(bank tenderLeg, amount float64) []postingLine {
	leg := bank
	leg.Amount = amount
	if leg.Role == "" {
		leg.Role = roleBank
	}
	return []postingLine{leg.debit(), creditRole(roleChequesInHand, amount, "cheque cleared")}
}

// chequeBounceLines puts a dishonoured cheque back onto the customer, together
// with any charge the bank levied.
//
//	Dr  accounts receivable
//	Dr  bank charges
//	    Cr  bank / cheques in hand
func chequeBounceLines(source tenderLeg, amount, charge float64) []postingLine {
	leg := source
	leg.Amount = money(amount + charge)
	if leg.Role == "" {
		leg.Role = roleBank
	}
	return []postingLine{
		debitRole(roleARControl, amount, "cheque dishonoured"),
		debitRole(roleBankCharges, charge, "return charge"),
		leg.credit(),
	}
}

// ---------------------------------------------------------------- expenses

// expenseLines records an operating expense. Pass the category's own ledger
// account when it has one, otherwise the generic operating-expense role is
// used.
//
//	Dr  expense account
//	Dr  input tax
//	    Cr  cash / bank / accounts payable
func expenseLines(expenseAccountID string, net, tax float64, paid []tenderLeg, onCredit float64) ([]postingLine, error) {
	expense := debitRole(roleOperatingExpense, net, "expense")
	if expenseAccountID != "" {
		expense = debitAccount(expenseAccountID, net, "expense")
	}
	lines := []postingLine{expense, debitRole(roleTaxInput, tax, "input tax")}
	settled := 0.0
	for _, t := range paid {
		if money(t.Amount) == 0 {
			continue
		}
		lines = append(lines, t.credit())
		settled += t.Amount
	}
	if money(onCredit) != 0 {
		lines = append(lines, creditRole(roleAPControl, onCredit, "expense on credit"))
		settled += onCredit
	}
	if money(settled) != money(net+tax) {
		return nil, fmt.Errorf(
			"expense settlement %.2f does not match expense total %.2f", settled, money(net+tax))
	}
	return lines, nil
}

// ------------------------------------------------------------ cash movement

// cashTransferLines moves money between two of the business's own accounts: a
// bank deposit, a withdrawal for the till, or a transfer between drawers.
//
//	Dr  destination
//	    Cr  source
func cashTransferLines(from, to tenderLeg, amount float64) []postingLine {
	source, destination := from, to
	source.Amount, destination.Amount = amount, amount
	return []postingLine{destination.debit(), source.credit()}
}

// cashierVarianceLines records the difference a cashier counted at close.
// A shortage is an expense; an overage is other income.
//
//	shortage: Dr cash short      Cr cash
//	overage:  Dr cash            Cr cash over
func cashierVarianceLines(drawer tenderLeg, counted, expected float64) []postingLine {
	variance := money(counted - expected)
	if variance == 0 {
		return nil
	}
	leg := drawer
	leg.Amount = math.Abs(variance)
	if variance < 0 {
		return []postingLine{
			debitRole(roleCashShort, leg.Amount, "cash short at close"),
			leg.credit(),
		}
	}
	return []postingLine{leg.debit(), creditRole(roleCashOver, leg.Amount, "cash over at close")}
}

// --------------------------------------------------------------- inventory

// inventoryWriteOffLines charges stock that has been lost to the reason that
// caused it, so damage, expiry, shrinkage and transfer shortages can each be
// reported on their own.
//
//	Dr  loss account for the reason
//	    Cr  inventory
func inventoryWriteOffLines(reason string, cost float64) []postingLine {
	return []postingLine{
		debitRole(lossRoleFor(reason), cost, reason+" loss"),
		creditRole(roleInventoryControl, cost, "stock written off"),
	}
}

// inventoryGainLines records stock found in excess of the book quantity.
//
//	Dr  inventory
//	    Cr  inventory gain
func inventoryGainLines(cost float64) []postingLine {
	return []postingLine{
		debitRole(roleInventoryControl, cost, "stock found"),
		creditRole(roleInventoryGain, cost, "inventory gain"),
	}
}

// lossRoleFor maps a stock-loss reason onto its expense account. Anything
// unrecognised lands in the general inventory-loss account rather than being
// rejected, so a new reason code can never block a stock movement.
func lossRoleFor(reason string) string {
	switch reason {
	case "damage", "damaged", "breakage":
		return roleDamageLoss
	case "expiry", "expired":
		return roleExpiryLoss
	case "adjustment", "shrinkage", "count":
		return roleAdjustmentLoss
	case "transfer", "transfer_shortage", "shortage":
		return roleTransferShortage
	default:
		return roleInventoryLoss
	}
}

// goodsInTransitOutLines and goodsInTransitInLines carry a branch-to-branch
// transfer, so stock in the lorry is still on the balance sheet but is not
// counted as available at either branch.
func goodsInTransitOutLines(cost float64) []postingLine {
	return []postingLine{
		debitRole(roleGoodsInTransit, cost, "transfer despatched"),
		creditRole(roleInventoryControl, cost, "out of branch stock"),
	}
}

func goodsInTransitInLines(cost float64) []postingLine {
	return []postingLine{
		debitRole(roleInventoryControl, cost, "transfer received"),
		creditRole(roleGoodsInTransit, cost, "cleared from transit"),
	}
}

// ---------------------------------------------------------------- openings

// openingBalanceLines sets an account's opening position against opening
// balance equity, which nets to zero once every opening balance is entered.
//
//	debit balance:  Dr account   Cr opening balance equity
//	credit balance: Dr opening balance equity   Cr account
func openingBalanceLines(accountID string, debit, credit float64) []postingLine {
	if money(debit) > 0 {
		return []postingLine{
			debitAccount(accountID, debit, "opening balance"),
			creditRole(roleOpeningBalanceEqty, debit, "opening balance"),
		}
	}
	return []postingLine{
		debitRole(roleOpeningBalanceEqty, credit, "opening balance"),
		creditAccount(accountID, credit, "opening balance"),
	}
}
