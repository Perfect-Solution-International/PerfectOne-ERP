package main

// Opening balances.
//
// Two ways in: state one account's opening position directly, or take on the
// balances that already sit in the operational tables. The second exists
// because customers, suppliers and stock can be loaded into the system before
// anyone posts the matching journals, which leaves every control account
// disagreeing with the subledger it is supposed to summarise.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type openingBalanceInput struct {
	AccountID   string  `json:"accountId"`
	Date        string  `json:"date"`
	Description string  `json:"description"`
	RequestID   string  `json:"requestId"`
	Amount      float64 `json:"amount"`
}

// createOpeningBalance posts one account's opening position against opening
// balance equity.
//
// It used to balance against Owner Equity, which mixes money the owner put in
// with balances carried over from before the system was running. Opening
// balance equity is the account that nets to zero once every opening position
// has been entered, so a non-zero balance on it is itself the signal that the
// take-on is incomplete.
func createOpeningBalance(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x openingBalanceInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || x.AccountID == "" || x.Amount == 0 || strings.TrimSpace(x.RequestID) == "" {
			http.Error(w, "account, non-zero amount and request ID are required", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())

		var normal string
		if err = tx.QueryRow(r.Context(),
			`SELECT normal_balance FROM chart_of_accounts
			  WHERE id=$1::uuid AND tenant_id=$2 AND is_active AND NOT is_group`,
			x.AccountID, c.Tenant).Scan(&normal); err != nil {
			http.Error(w, "account not found, inactive, or a heading", 404)
			return
		}

		// A negative amount means the account opens on the side opposite to
		// its nature, which is legitimate: an overdrawn bank account, say.
		debit, credit := x.Amount, 0.0
		if normal == "credit" {
			debit, credit = 0, x.Amount
		}
		if debit < 0 {
			debit, credit = 0, -debit
		} else if credit < 0 {
			debit, credit = -credit, 0
		}

		description := strings.TrimSpace(x.Description)
		if description == "" {
			description = "Opening balance"
		}
		journalID, entryNo, err := postJournalTx(r.Context(), tx, journalDraft{
			Tenant: c.Tenant, Branch: c.Branch, Actor: c.Sub,
			Date:            x.Date,
			ReferenceType:   "opening_balance",
			Description:     description,
			Source:          "opening",
			ClientRequestID: x.RequestID,
			Lines:           openingBalanceLines(x.AccountID, debit, credit),
		})
		if err != nil {
			http.Error(w, "opening balance could not be posted: "+err.Error(), 409)
			return
		}

		// The register enforces one opening per account per date, so a repeated
		// take-on cannot silently double an account's opening position.
		if _, err = tx.Exec(r.Context(), `
			INSERT INTO accounting_opening_register(tenant_id, account_id, opening_date, journal_id, created_by)
			VALUES($1, $2::uuid, COALESCE(NULLIF($3,'')::date, current_date), $4::uuid, NULLIF($5,'')::uuid)`,
			c.Tenant, x.AccountID, x.Date, journalID, c.Sub); err != nil {
			http.Error(w, "this account already has an opening balance on that date", 409)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		auditUserAction(r, db, "OPENING_BALANCE_POSTED", journalID, map[string]any{"amount": x.Amount})
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"id": journalID, "entryNo": entryNo})
	}
}

type openingSyncInput struct {
	Date      string `json:"date"`
	Reason    string `json:"reason"`
	RequestID string `json:"requestId"`
	Preview   bool   `json:"preview"`
}

// syncOpeningBalances closes the gap between each control account and the
// subledger that owns it, in one journal balanced against opening balance
// equity.
//
// This is the take-on an accountant runs once, after loading customers,
// suppliers and stock, to bring the general ledger up to the position the
// operational tables already describe. It is deliberately driven by the same
// reconciliation the discrepancy alerts read, so running it drives those
// alerts to zero by construction rather than by a second calculation that
// might disagree.
func syncOpeningBalances(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x openingSyncInput
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.Reason) == "" {
			http.Error(w, "a reason is required", 400)
			return
		}
		if !x.Preview && strings.TrimSpace(x.RequestID) == "" {
			http.Error(w, "request ID is required", 400)
			return
		}
		c := claimsFrom(r)
		tx, err := db.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback(r.Context())

		// The gap is measured as it stands today, not as at the opening date:
		// the subledger tables hold current balances, so asking what they were
		// on an earlier date would compare today's customer balances against a
		// part of the ledger and take on far too much. The opening date is only
		// where the correcting journal is dated.
		rows, err := tx.Query(r.Context(), `
			SELECT rec.account_id::text, rec.code, rec.name, a.normal_balance, rec.difference
			  FROM accounting_control_reconciliation($1, current_date) rec
			  JOIN chart_of_accounts a ON a.id = rec.account_id
			 WHERE rec.subledger IS NOT NULL AND NOT rec.balanced
			 ORDER BY rec.code`, c.Tenant)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		lines := []postingLine{}
		adjustments := []map[string]any{}
		net := 0.0
		for rows.Next() {
			var accountID, code, name, normal string
			var difference float64
			if err := rows.Scan(&accountID, &code, &name, &normal, &difference); err != nil {
				rows.Close()
				http.Error(w, err.Error(), 500)
				return
			}
			// difference is ledger minus subledger, so a negative difference
			// means the ledger is short on the account's natural side.
			needed := -difference
			debit, credit := 0.0, 0.0
			switch {
			case normal == "debit" && needed > 0:
				debit = needed
			case normal == "debit":
				credit = -needed
			case needed > 0:
				credit = needed
			default:
				debit = -needed
			}
			memo := "opening take-on"
			if debit > 0 {
				lines = append(lines, debitAccount(accountID, debit, memo))
			} else {
				lines = append(lines, creditAccount(accountID, credit, memo))
			}
			net += debit - credit
			adjustments = append(adjustments, map[string]any{
				"code": code, "name": name, "debit": money(debit), "credit": money(credit),
			})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if len(lines) == 0 {
			json.NewEncoder(w).Encode(map[string]any{
				"posted": false, "adjustments": adjustments,
				"message": "every control account already agrees with its subledger",
			})
			return
		}

		// opening balance equity takes the other side of the whole take-on
		if net > 0 {
			lines = append(lines, creditRole(roleOpeningBalanceEqty, net, "opening take-on"))
		} else {
			lines = append(lines, debitRole(roleOpeningBalanceEqty, -net, "opening take-on"))
		}

		// report the equity side the same way as the adjustments, so the client
		// never has to work out what the sign of a single number means
		equitySide := map[string]any{"name": "Opening Balance Equity", "debit": 0.0, "credit": money(net)}
		if net < 0 {
			equitySide = map[string]any{"name": "Opening Balance Equity", "debit": money(-net), "credit": 0.0}
		}
		if x.Preview {
			json.NewEncoder(w).Encode(map[string]any{
				"posted": false, "adjustments": adjustments, "balancingEntry": equitySide,
			})
			return
		}

		journalID, entryNo, err := postJournalTx(r.Context(), tx, journalDraft{
			Tenant: c.Tenant, Branch: c.Branch, Actor: c.Sub,
			Date:            x.Date,
			ReferenceType:   "opening_balance",
			Description:     fmt.Sprintf("Opening take-on / %s", strings.TrimSpace(x.Reason)),
			Source:          "opening",
			ClientRequestID: x.RequestID,
			Lines:           lines,
		})
		if err != nil {
			http.Error(w, "take-on could not be posted: "+err.Error(), 409)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		auditUserAction(r, db, "OPENING_BALANCES_SYNCED", journalID, map[string]any{
			"reason": x.Reason, "accounts": len(adjustments),
		})
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{
			"posted": true, "id": journalID, "entryNo": entryNo,
			"adjustments": adjustments, "balancingEntry": equitySide,
		})
	}
}
