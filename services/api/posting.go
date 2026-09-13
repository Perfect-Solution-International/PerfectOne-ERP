package main

// Double-entry posting engine.
//
// Every journal the system writes goes through postJournalTx. Callers describe
// the entry in terms of posting roles ("ar_control", "tax_output") rather than
// account codes; the engine resolves those through the account_roles table, so
// a tenant can renumber or re-map its chart without a code change.
//
// The engine is transaction-scoped on purpose: it takes the caller's pgx.Tx so
// that stock movement, payment capture and the journal either all commit or all
// roll back together.

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Posting roles. These mirror the rows seeded into account_roles by migration
// 064; resolving an unseeded role is an error rather than a silent skip.
const (
	roleCashOnHand         = "cash_on_hand"
	rolePettyCash          = "petty_cash"
	roleBank               = "bank"
	roleChequesInHand      = "cheques_in_hand"
	roleARControl          = "ar_control"
	roleSupplierAdvances   = "supplier_advances"
	roleInventoryControl   = "inventory_control"
	roleGoodsInTransit     = "goods_in_transit"
	roleTaxInput           = "tax_input"
	roleSuspense           = "suspense"
	roleAPControl          = "ap_control"
	roleGRNI               = "grni"
	roleCustomerAdvances   = "customer_advances"
	roleTaxOutput          = "tax_output"
	roleOwnerEquity        = "owner_equity"
	roleRetainedEarnings   = "retained_earnings"
	roleOpeningBalanceEqty = "opening_balance_equity"
	roleSalesRevenue       = "sales_revenue"
	roleSalesReturns       = "sales_returns"
	roleSalesDiscount      = "sales_discount"
	roleInventoryGain      = "inventory_gain"
	roleCashOver           = "cash_over"
	roleCOGS               = "cogs"
	roleFreightInwards     = "freight_inwards"
	rolePurchaseDiscount   = "purchase_discount"
	roleOperatingExpense   = "operating_expense"
	roleBankCharges        = "bank_charges"
	roleCashShort          = "cash_short"
	roleInventoryLoss      = "inventory_loss"
	roleDamageLoss         = "damage_loss"
	roleExpiryLoss         = "expiry_loss"
	roleAdjustmentLoss     = "adjustment_loss"
	roleTransferShortage   = "transfer_shortage"
)

// money rounds to the two decimal places every ledger column stores, so that
// float arithmetic upstream cannot push a journal a cent out of balance.
func money(v float64) float64 { return math.Round(v*100) / 100 }

// postingLine is one leg of an entry. Give it either a Role or an explicit
// AccountID; AccountID wins when both are set, which is how per-drawer cash
// accounts and per-category expense accounts post to their own ledger account.
type postingLine struct {
	Role      string
	AccountID string
	Debit     float64
	Credit    float64
	Memo      string
}

func debitRole(role string, amount float64, memo string) postingLine {
	return postingLine{Role: role, Debit: amount, Memo: memo}
}

func creditRole(role string, amount float64, memo string) postingLine {
	return postingLine{Role: role, Credit: amount, Memo: memo}
}

func debitAccount(accountID string, amount float64, memo string) postingLine {
	return postingLine{AccountID: accountID, Debit: amount, Memo: memo}
}

func creditAccount(accountID string, amount float64, memo string) postingLine {
	return postingLine{AccountID: accountID, Credit: amount, Memo: memo}
}

// journalDraft is a complete entry waiting to be written.
type journalDraft struct {
	Tenant          string
	Branch          string // optional; empty posts at company level
	Actor           string // user id, empty for unattended system posting
	Date            string // YYYY-MM-DD; empty means today
	ReferenceType   string // document type, e.g. "sale" or "purchase_return"
	ReferenceID     string // the document's id
	Description     string
	Source          string // "system" (default) or "manual"
	ClientRequestID string // idempotency key; a repeat returns the first entry
	Lines           []postingLine
}

// prepare drops no-op legs, rounds the rest and proves the entry balances.
func (d *journalDraft) prepare() ([]postingLine, error) {
	kept := make([]postingLine, 0, len(d.Lines))
	debit, credit := 0.0, 0.0
	for _, l := range d.Lines {
		l.Debit, l.Credit = money(l.Debit), money(l.Credit)
		if l.Debit == 0 && l.Credit == 0 {
			continue // a zero tax or discount leg simply does not exist
		}
		if l.Debit < 0 || l.Credit < 0 {
			return nil, fmt.Errorf("posting amounts cannot be negative; reverse the sides instead")
		}
		if l.Debit > 0 && l.Credit > 0 {
			return nil, fmt.Errorf("a posting line carries either a debit or a credit, not both")
		}
		if strings.TrimSpace(l.Role) == "" && strings.TrimSpace(l.AccountID) == "" {
			return nil, fmt.Errorf("posting line needs a role or an account")
		}
		debit += l.Debit
		credit += l.Credit
		kept = append(kept, l)
	}
	if len(kept) < 2 {
		return nil, fmt.Errorf("a journal needs at least two lines")
	}
	if money(debit) != money(credit) {
		return nil, fmt.Errorf("journal is out of balance: debits %.2f, credits %.2f", debit, credit)
	}
	if money(debit) == 0 {
		return nil, fmt.Errorf("journal has no value")
	}
	return kept, nil
}

// resolveRoles maps every role used by the draft to an account id in one query.
func resolveRoles(ctx context.Context, tx pgx.Tx, tenant string, lines []postingLine) (map[string]string, error) {
	wanted := map[string]bool{}
	for _, l := range lines {
		if l.AccountID == "" && l.Role != "" {
			wanted[l.Role] = true
		}
	}
	if len(wanted) == 0 {
		return map[string]string{}, nil
	}
	names := make([]string, 0, len(wanted))
	for r := range wanted {
		names = append(names, r)
	}
	sort.Strings(names)
	rows, err := tx.Query(ctx,
		`SELECT role, account_id::text FROM account_roles WHERE tenant_id=$1 AND role = ANY($2)`,
		tenant, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[string]string{}
	for rows.Next() {
		var role, account string
		if err := rows.Scan(&role, &account); err != nil {
			return nil, err
		}
		found[role] = account
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	missing := []string{}
	for _, r := range names {
		if found[r] == "" {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no account is mapped to posting role(s): %s", strings.Join(missing, ", "))
	}
	return found, nil
}

// postJournalTx writes a balanced entry inside the caller's transaction and
// returns its id and entry number.
//
// When ClientRequestID repeats an entry this tenant already has, the existing
// entry is returned untouched. That makes a retried checkout or a replayed
// offline sale safe: the caller gets the original journal rather than a double
// posting.
func postJournalTx(ctx context.Context, tx pgx.Tx, d journalDraft) (string, string, error) {
	lines, err := d.prepare()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(d.Tenant) == "" {
		return "", "", fmt.Errorf("journal needs a tenant")
	}
	if strings.TrimSpace(d.Description) == "" {
		return "", "", fmt.Errorf("journal needs a description")
	}
	source := strings.TrimSpace(d.Source)
	if source == "" {
		source = "system"
	}

	if key := strings.TrimSpace(d.ClientRequestID); key != "" {
		var id, no string
		err := tx.QueryRow(ctx,
			`SELECT id::text, COALESCE(entry_no,'') FROM journal_entries
			  WHERE tenant_id=$1 AND client_request_id=$2`, d.Tenant, key).Scan(&id, &no)
		if err == nil {
			return id, no, nil
		}
		if err != pgx.ErrNoRows {
			return "", "", err
		}
	}

	accounts, err := resolveRoles(ctx, tx, d.Tenant, lines)
	if err != nil {
		return "", "", err
	}

	var id, entryNo string
	err = tx.QueryRow(ctx, `
		INSERT INTO journal_entries
		  (tenant_id, branch_id, entry_date, reference_type, reference_id, description,
		   created_by, posted_by, status, posted_at, source, client_request_id)
		VALUES
		  ($1, NULLIF($2,'')::uuid, COALESCE(NULLIF($3,'')::date, current_date),
		   NULLIF($4,''), NULLIF($5,'')::uuid, $6,
		   NULLIF($7,'')::uuid, NULLIF($7,'')::uuid, 'posted', now(), $8, NULLIF($9,''))
		RETURNING id::text, COALESCE(entry_no,'')`,
		d.Tenant, strings.TrimSpace(d.Branch), strings.TrimSpace(d.Date),
		strings.TrimSpace(d.ReferenceType), strings.TrimSpace(d.ReferenceID),
		strings.TrimSpace(d.Description), strings.TrimSpace(d.Actor), source,
		strings.TrimSpace(d.ClientRequestID)).Scan(&id, &entryNo)
	if err != nil {
		return "", "", fmt.Errorf("journal header rejected: %w", err)
	}

	for _, l := range lines {
		account := l.AccountID
		if account == "" {
			account = accounts[l.Role]
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO journal_lines(journal_id, account_id, debit, credit, memo)
			SELECT $1, a.id, $3, $4, NULLIF($5,'')
			  FROM chart_of_accounts a
			 WHERE a.id=$2::uuid AND a.tenant_id=$6 AND a.is_active AND NOT a.is_group`,
			id, account, l.Debit, l.Credit, strings.TrimSpace(l.Memo), d.Tenant)
		if err != nil {
			return "", "", fmt.Errorf("journal line rejected: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return "", "", fmt.Errorf("account %s is inactive, a heading, or belongs to another tenant", account)
		}
	}
	return id, entryNo, nil
}

// reverseJournalTx writes the mirror image of a posted entry and marks the
// original reversed. Used by invoice cancellation, purchase reversal, payment
// reversal and any correction that must leave the original entry visible.
func reverseJournalTx(ctx context.Context, tx pgx.Tx, tenant, actor, journalID, reason string) (string, string, error) {
	if strings.TrimSpace(reason) == "" {
		return "", "", fmt.Errorf("a reversal needs a reason")
	}
	var refType, refID, description, branch, entryDate string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(reference_type,''), COALESCE(reference_id::text,''),
		       COALESCE(description,''), COALESCE(branch_id::text,''), entry_date::text
		  FROM journal_entries
		 WHERE id=$1::uuid AND tenant_id=$2 AND status='posted'
		 FOR UPDATE`, journalID, tenant).Scan(&refType, &refID, &description, &branch, &entryDate)
	if err == pgx.ErrNoRows {
		return "", "", fmt.Errorf("only a posted journal can be reversed")
	}
	if err != nil {
		return "", "", err
	}

	rows, err := tx.Query(ctx,
		`SELECT account_id::text, debit, credit, COALESCE(memo,'') FROM journal_lines WHERE journal_id=$1::uuid`,
		journalID)
	if err != nil {
		return "", "", err
	}
	mirrored := []postingLine{}
	for rows.Next() {
		var account, memo string
		var debit, credit float64
		if err := rows.Scan(&account, &debit, &credit, &memo); err != nil {
			rows.Close()
			return "", "", err
		}
		// sides swap; the memo keeps the original wording so the pair reads together
		mirrored = append(mirrored, postingLine{AccountID: account, Debit: credit, Credit: debit, Memo: memo})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	if len(mirrored) == 0 {
		return "", "", fmt.Errorf("journal has no lines to reverse")
	}

	reversalID, reversalNo, err := postJournalTx(ctx, tx, journalDraft{
		Tenant:        tenant,
		Branch:        branch,
		Actor:         actor,
		Date:          entryDate,
		ReferenceType: refType,
		ReferenceID:   refID,
		Description:   "Reversal of " + description + " / " + strings.TrimSpace(reason),
		Source:        "system",
		Lines:         mirrored,
	})
	if err != nil {
		return "", "", err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE journal_entries
		   SET reversed_entry_id=$3::uuid, reversal_reason=$4, reversed_by=NULLIF($5,'')::uuid
		 WHERE id=$1::uuid AND tenant_id=$2 AND status='posted' AND reversed_entry_id IS NULL`,
		journalID, tenant, reversalID, strings.TrimSpace(reason), strings.TrimSpace(actor))
	if err != nil {
		return "", "", err
	}
	if tag.RowsAffected() != 1 {
		return "", "", fmt.Errorf("journal could not be marked reversed")
	}
	return reversalID, reversalNo, nil
}

// accountForRole resolves a single posting role, for callers that need the
// account id itself rather than a whole entry.
func accountForRole(ctx context.Context, tx pgx.Tx, tenant, role string) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`SELECT account_id::text FROM account_roles WHERE tenant_id=$1 AND role=$2`, tenant, role).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("no account is mapped to posting role %q", role)
	}
	return id, err
}
