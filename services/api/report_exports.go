package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
)

func registerReportExports(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /reports/{name}", jsonAPI(authenticated(db, financeRoles...)(reportExport(db))))
}
func reportExport(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		search := "%" + r.URL.Query().Get("search") + "%"
		tenant := claimsFrom(r).Tenant
		q := ""
		args := []any{}
		headers := []string{}
		switch name {
		case "sales":
			q = `SELECT invoice_no,COALESCE(c.name,'Walk-in'),total-returned_total,LEAST(paid_amount,total-returned_total),status,created_at::date FROM sales s LEFT JOIN customers c ON c.id=s.customer_id WHERE s.created_at::date>=COALESCE(NULLIF($1,'')::date,'2000-01-01') AND s.created_at::date<=COALESCE(NULLIF($2,'')::date,current_date) AND s.status<>'cancelled' AND (s.invoice_no ILIKE $3 OR COALESCE(c.name,'') ILIKE $3) AND s.tenant_id=$4 ORDER BY s.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Invoice", "Customer", "Net total", "Net paid", "Status", "Date"}
		case "purchases":
			q = `SELECT p.invoice_no,s.name,p.total,p.paid_amount,p.status,p.purchase_date FROM purchases p JOIN suppliers s ON s.id=p.supplier_id WHERE p.purchase_date>=COALESCE(NULLIF($1,'')::date,'2000-01-01') AND p.purchase_date<=COALESCE(NULLIF($2,'')::date,current_date) AND p.status IN('finalized','reversed') AND (p.invoice_no ILIKE $3 OR s.name ILIKE $3) AND p.tenant_id=$4 ORDER BY p.purchase_date DESC,p.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Invoice", "Supplier", "Total", "Paid", "Status", "Date"}
		case "inventory":
			q = `SELECT sku,name,stock_quantity,minimum_stock,stock_quantity*purchase_price FROM products WHERE is_active AND (name ILIKE $1 OR sku ILIKE $1) AND tenant_id=$2 ORDER BY name`
			args = []any{search, tenant}
			headers = []string{"SKU", "Product", "Stock", "Minimum", "Stock value"}
		case "low-stock":
			q = `SELECT sku,name,stock_quantity,minimum_stock FROM products WHERE is_active AND stock_quantity<=minimum_stock AND (name ILIKE $1 OR sku ILIKE $1) AND tenant_id=$2 ORDER BY stock_quantity`
			args = []any{search, tenant}
			headers = []string{"SKU", "Product", "Stock", "Minimum"}
		case "expiry":
			q = `SELECT p.name,COALESCE(b.batch_no,''),b.expiry_date,b.available_qty FROM stock_batches b JOIN products p ON p.id=b.product_id WHERE b.available_qty>0 AND b.expiry_date<=COALESCE(NULLIF($2,'')::date,current_date+30) AND b.expiry_date>=COALESCE(NULLIF($1,'')::date,current_date) AND p.name ILIKE $3 AND p.tenant_id=$4 ORDER BY b.expiry_date`
			args = []any{from, to, search, tenant}
			headers = []string{"Product", "Batch", "Expiry", "Available"}
		case "customers":
			q = `SELECT name,phone,credit_limit,balance FROM customers WHERE name ILIKE $1 AND tenant_id=$2 ORDER BY name`
			args = []any{search, tenant}
			headers = []string{"Customer", "Phone", "Credit limit", "Outstanding"}
		case "suppliers":
			q = `SELECT name,phone,balance FROM suppliers WHERE name ILIKE $1 AND tenant_id=$2 ORDER BY name`
			args = []any{search, tenant}
			headers = []string{"Supplier", "Phone", "Outstanding"}
		case "cash-flow":
			q = `SELECT a.name,t.transaction_type,t.amount,t.description,t.transaction_date FROM cash_transactions t JOIN cash_accounts a ON a.id=t.account_id WHERE t.transaction_date>=COALESCE(NULLIF($1,'')::date,'2000-01-01') AND t.transaction_date<=COALESCE(NULLIF($2,'')::date,current_date) AND (a.name ILIKE $3 OR COALESCE(t.description,'') ILIKE $3) AND a.tenant_id=$4 ORDER BY t.transaction_date DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Account", "Type", "Amount", "Description", "Date"}
		case "cashier-sales":
			q = `SELECT COALESCE(u.name,'Unknown'),count(*),COALESCE(sum(s.total-s.returned_total),0),COALESCE(sum(LEAST(s.paid_amount,s.total-s.returned_total)),0) FROM sales s LEFT JOIN users u ON u.id=s.cashier_id WHERE s.tenant_id=$4 AND s.status<>'cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND COALESCE(u.name,'') ILIKE $3 GROUP BY u.id,u.name ORDER BY 3 DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Cashier", "Sales count", "Net sales", "Collected"}
		case "product-sales":
			q = `SELECT p.sku,p.name,sum(i.quantity),sum(i.quantity*i.unit_price-i.discount),sum(i.quantity*i.unit_cost) FROM sale_items i JOIN sales s ON s.id=i.sale_id JOIN products p ON p.id=i.product_id WHERE s.tenant_id=$4 AND s.status<>'cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (p.name ILIKE $3 OR p.sku ILIKE $3) GROUP BY p.id ORDER BY 4 DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"SKU", "Product", "Quantity", "Net sales", "Cost"}
		case "category-sales":
			q = `SELECT COALESCE(c.name,'Uncategorised'),sum(i.quantity),sum(i.quantity*i.unit_price-i.discount),sum(i.quantity*i.unit_cost) FROM sale_items i JOIN sales s ON s.id=i.sale_id JOIN products p ON p.id=i.product_id LEFT JOIN categories c ON c.id=p.category_id WHERE s.tenant_id=$4 AND s.status<>'cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND COALESCE(c.name,'Uncategorised') ILIKE $3 GROUP BY c.id,c.name ORDER BY 3 DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Category", "Quantity", "Net sales", "Cost"}
		case "customer-sales":
			q = `SELECT COALESCE(c.name,'Walk-in'),count(*),sum(s.total-s.returned_total),sum(LEAST(s.paid_amount,s.total-s.returned_total)),sum(GREATEST(s.balance-s.returned_total,0)) FROM sales s LEFT JOIN customers c ON c.id=s.customer_id WHERE s.tenant_id=$4 AND s.status<>'cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND COALESCE(c.name,'Walk-in') ILIKE $3 GROUP BY c.id,c.name ORDER BY 3 DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Customer", "Invoices", "Net sales", "Paid", "Outstanding"}
		case "stock-movements":
			q = `SELECT m.created_at::date,p.sku,p.name,m.kind,m.quantity,m.balance_after,COALESCE(m.reference_type,''),COALESCE(m.notes,'') FROM stock_movements m JOIN products p ON p.id=m.product_id WHERE m.tenant_id=$4 AND m.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (p.name ILIKE $3 OR p.sku ILIKE $3 OR m.kind ILIKE $3) ORDER BY m.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "SKU", "Product", "Movement", "Quantity", "Balance", "Reference", "Notes"}
		case "stock-adjustments":
			q = `SELECT a.created_at::date,p.sku,p.name,a.system_quantity,a.physical_quantity,a.quantity,a.reason,a.status,COALESCE(u.name,'System') FROM stock_adjustments a JOIN products p ON p.id=a.product_id LEFT JOIN users u ON u.id=a.adjusted_by WHERE a.tenant_id=$4 AND a.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (p.name ILIKE $3 OR p.sku ILIKE $3 OR a.reason ILIKE $3) ORDER BY a.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "SKU", "Product", "System", "Physical", "Adjustment", "Reason", "Status", "User"}
		case "damaged-stock":
			q = `SELECT d.created_at::date,p.sku,p.name,d.quantity,d.reason,d.status,COALESCE(u.name,'System') FROM damaged_items d JOIN products p ON p.id=d.product_id LEFT JOIN users u ON u.id=d.reported_by WHERE d.tenant_id=$4 AND d.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (p.name ILIKE $3 OR p.sku ILIKE $3 OR d.reason ILIKE $3) ORDER BY d.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "SKU", "Product", "Quantity", "Reason", "Status", "User"}
		case "customer-payments":
			q = `SELECT p.payment_date,c.name,p.amount,p.method,COALESCE(p.reference,''),p.status FROM customer_payments p JOIN customers c ON c.id=p.customer_id WHERE p.tenant_id=$4 AND p.payment_date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (c.name ILIKE $3 OR COALESCE(p.reference,'') ILIKE $3) ORDER BY p.payment_date DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Customer", "Amount", "Method", "Reference", "Status"}
		case "supplier-payments":
			q = `SELECT p.payment_date,s.name,p.amount,p.method,COALESCE(p.reference,''),p.status FROM supplier_payments p JOIN suppliers s ON s.id=p.supplier_id WHERE p.tenant_id=$4 AND p.payment_date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (s.name ILIKE $3 OR COALESCE(p.reference,'') ILIKE $3) ORDER BY p.payment_date DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Supplier", "Amount", "Method", "Reference", "Status"}
		case "purchase-returns":
			q = `SELECT r.return_date,r.return_number,p.invoice_no,s.name,r.total,r.reason,r.status FROM purchase_returns r JOIN purchases p ON p.id=r.purchase_id JOIN suppliers s ON s.id=r.supplier_id WHERE r.tenant_id=$4 AND r.return_date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (r.return_number ILIKE $3 OR p.invoice_no ILIKE $3 OR s.name ILIKE $3) ORDER BY r.return_date DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Return", "Purchase invoice", "Supplier", "Total", "Reason", "Status"}
		case "expenses":
			q = `SELECT e.expense_date,c.name,e.description,e.amount,e.payment_method,e.status,COALESCE(e.reference,'') FROM expenses e JOIN expense_categories c ON c.id=e.category_id WHERE e.tenant_id=$4 AND e.expense_date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (c.name ILIKE $3 OR e.description ILIKE $3 OR COALESCE(e.reference,'') ILIKE $3) ORDER BY e.expense_date DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Category", "Description", "Amount", "Method", "Status", "Reference"}
		case "trial-balance":
			q = `SELECT a.code,a.name,a.account_type,COALESCE(sum(jl.debit) FILTER(WHERE je.id IS NOT NULL),0),COALESCE(sum(jl.credit) FILTER(WHERE je.id IS NOT NULL),0),COALESCE(sum(jl.debit-jl.credit) FILTER(WHERE je.id IS NOT NULL),0) FROM chart_of_accounts a LEFT JOIN journal_lines jl ON jl.account_id=a.id LEFT JOIN journal_entries je ON je.id=jl.journal_id AND je.status='posted' AND je.entry_date>=COALESCE(NULLIF($1,'')::date,'2000-01-01') AND je.entry_date<=COALESCE(NULLIF($2,'')::date,current_date) WHERE a.tenant_id=$4 AND (a.code ILIKE $3 OR a.name ILIKE $3) GROUP BY a.id ORDER BY a.code`
			args = []any{from, to, search, tenant}
			headers = []string{"Code", "Account", "Type", "Debit", "Credit", "Debit/(Credit) balance"}
		case "profit-loss":
			q = `SELECT a.code,a.name,a.account_type,COALESCE(sum(jl.debit),0),COALESCE(sum(jl.credit),0),COALESCE(sum(CASE WHEN a.account_type='income' THEN jl.credit-jl.debit ELSE jl.debit-jl.credit END),0) FROM chart_of_accounts a JOIN journal_lines jl ON jl.account_id=a.id JOIN journal_entries je ON je.id=jl.journal_id WHERE a.tenant_id=$4 AND je.status='posted' AND a.account_type IN('income','expense') AND je.entry_date>=COALESCE(NULLIF($1,'')::date,'2000-01-01') AND je.entry_date<=COALESCE(NULLIF($2,'')::date,current_date) AND (a.code ILIKE $3 OR a.name ILIKE $3) GROUP BY a.id ORDER BY a.account_type,a.code`
			args = []any{from, to, search, tenant}
			headers = []string{"Code", "Account", "Type", "Debit", "Credit", "Amount"}
		case "balance-sheet":
			q = `SELECT code,name,account_type,debit,credit,balance FROM (SELECT a.code,a.name,a.account_type,COALESCE(sum(jl.debit) FILTER(WHERE je.id IS NOT NULL),0) debit,COALESCE(sum(jl.credit) FILTER(WHERE je.id IS NOT NULL),0) credit,COALESCE(sum(CASE WHEN a.account_type='asset' THEN jl.debit-jl.credit ELSE jl.credit-jl.debit END) FILTER(WHERE je.id IS NOT NULL),0) balance FROM chart_of_accounts a LEFT JOIN journal_lines jl ON jl.account_id=a.id LEFT JOIN journal_entries je ON je.id=jl.journal_id AND je.status='posted' AND je.entry_date<=COALESCE(NULLIF($2,'')::date,current_date) WHERE a.tenant_id=$4 AND a.account_type IN('asset','liability','equity') GROUP BY a.id UNION ALL SELECT '3999','Current Earnings','equity',0,0,COALESCE(sum(CASE WHEN a.account_type='income' THEN jl.credit-jl.debit ELSE jl.credit-jl.debit END),0) FROM journal_lines jl JOIN journal_entries je ON je.id=jl.journal_id JOIN chart_of_accounts a ON a.id=jl.account_id WHERE je.tenant_id=$4 AND je.status='posted' AND je.entry_date<=COALESCE(NULLIF($2,'')::date,current_date) AND a.account_type IN('income','expense')) x WHERE code ILIKE $3 OR name ILIKE $3 ORDER BY account_type,code`
			args = []any{from, to, search, tenant}
			headers = []string{"Code", "Account", "Type", "Debit", "Credit", "Balance"}
		case "general-ledger":
			q = `SELECT je.entry_date,a.code,a.name,COALESCE(je.reference_type,''),COALESCE(je.description,''),jl.debit,jl.credit,je.status FROM journal_lines jl JOIN journal_entries je ON je.id=jl.journal_id JOIN chart_of_accounts a ON a.id=jl.account_id WHERE je.tenant_id=$4 AND je.entry_date>=COALESCE(NULLIF($1,'')::date,'2000-01-01') AND je.entry_date<=COALESCE(NULLIF($2,'')::date,current_date) AND (a.code ILIKE $3 OR a.name ILIKE $3 OR COALESCE(je.description,'') ILIKE $3) ORDER BY je.entry_date DESC,je.created_at DESC,jl.id`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Code", "Account", "Reference type", "Description", "Debit", "Credit", "Status"}
		case "payment-method-sales":
			q = `SELECT payment_method,count(*),sum(total-returned_total),sum(paid_amount-returned_paid),sum(balance-returned_receivable) FROM sales WHERE tenant_id=$4 AND status<>'cancelled' AND created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND payment_method ILIKE $3 GROUP BY payment_method ORDER BY 3 DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Payment method", "Invoices", "Net sales", "Collected", "Receivable"}
		case "sales-returns":
			q = `SELECT r.created_at::date,r.return_no,s.invoice_no,COALESCE(c.name,'Walk-in'),r.total,r.reason,COALESCE(u.name,'Unknown') FROM sale_returns r JOIN sales s ON s.id=r.sale_id LEFT JOIN customers c ON c.id=s.customer_id LEFT JOIN users u ON u.id=r.returned_by WHERE s.tenant_id=$4 AND r.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (r.return_no ILIKE $3 OR s.invoice_no ILIKE $3 OR r.reason ILIKE $3) ORDER BY r.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Return", "Invoice", "Customer", "Amount", "Reason", "User"}
		case "cancelled-sales":
			q = `SELECT s.created_at::date,s.invoice_no,COALESCE(c.name,'Walk-in'),s.total,COALESCE(s.cancel_reason,''),COALESCE(u.name,'Unknown') FROM sales s LEFT JOIN customers c ON c.id=s.customer_id LEFT JOIN users u ON u.id=s.cashier_id WHERE s.tenant_id=$4 AND s.status='cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (s.invoice_no ILIKE $3 OR COALESCE(c.name,'') ILIKE $3 OR COALESCE(s.cancel_reason,'') ILIKE $3) ORDER BY s.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Invoice", "Customer", "Total", "Reason", "Cashier"}
		case "purchase-orders":
			q = `SELECT p.order_date,p.po_number,s.name,p.expected_date,p.total,p.status,COALESCE(u.name,'Unknown') FROM purchase_orders p JOIN suppliers s ON s.id=p.supplier_id LEFT JOIN users u ON u.id=p.created_by WHERE p.tenant_id=$4 AND p.order_date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (p.po_number ILIKE $3 OR s.name ILIKE $3 OR p.status ILIKE $3) ORDER BY p.order_date DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "PO", "Supplier", "Expected", "Total", "Status", "Created by"}
		case "branch-stock":
			q = `SELECT COALESCE(b.name,'Primary'),p.sku,p.name,COALESCE(bs.quantity,0),p.minimum_stock,COALESCE(bs.quantity,0)*p.purchase_price FROM products p LEFT JOIN branch_stock bs ON bs.product_id=p.id LEFT JOIN branches b ON b.id=bs.branch_id WHERE p.tenant_id=$2 AND (p.name ILIKE $1 OR p.sku ILIKE $1 OR COALESCE(b.name,'Primary') ILIKE $1) ORDER BY 1,p.name`
			args = []any{search, tenant}
			headers = []string{"Branch", "SKU", "Product", "Quantity", "Minimum", "Value"}
		case "out-of-stock":
			q = `SELECT sku,name,minimum_stock,purchase_price,selling_price FROM products WHERE tenant_id=$2 AND is_active AND stock_quantity<=0 AND (name ILIKE $1 OR sku ILIKE $1) ORDER BY name`
			args = []any{search, tenant}
			headers = []string{"SKU", "Product", "Minimum", "Cost", "Selling price"}
		case "customer-ageing":
			q = `SELECT name,phone,credit_limit,balance,CASE WHEN balance<=0 THEN 'Clear' ELSE 'Outstanding' END FROM customers WHERE tenant_id=$2 AND balance<>0 AND (name ILIKE $1 OR phone ILIKE $1) ORDER BY balance DESC`
			args = []any{search, tenant}
			headers = []string{"Customer", "Phone", "Credit limit", "Outstanding", "Status"}
		case "supplier-ageing":
			q = `SELECT name,phone,credit_limit,balance,payment_terms FROM suppliers WHERE tenant_id=$2 AND balance<>0 AND (name ILIKE $1 OR phone ILIKE $1) ORDER BY balance DESC`
			args = []any{search, tenant}
			headers = []string{"Supplier", "Phone", "Credit limit", "Payable", "Terms"}
		case "journal-register":
			q = `SELECT je.entry_date,je.entry_no,COALESCE(je.reference_type,''),COALESCE(je.reference_id::text,''),je.description,je.status,COALESCE(u.name,'System') FROM journal_entries je LEFT JOIN users u ON u.id=je.created_by WHERE je.tenant_id=$4 AND je.entry_date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (je.entry_no ILIKE $3 OR je.description ILIKE $3 OR COALESCE(je.reference_type,'') ILIKE $3) ORDER BY je.entry_date DESC,je.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Journal", "Type", "Reference", "Description", "Status", "User"}
		case "audit-log":
			q = `SELECT a.created_at,a.action,a.entity_type,COALESCE(a.entity_id::text,''),COALESCE(u.name,'System'),COALESCE(a.ip_address::text,'') FROM audit_logs a LEFT JOIN users u ON u.id=a.user_id WHERE a.tenant_id=$4 AND a.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (a.action ILIKE $3 OR a.entity_type ILIKE $3 OR COALESCE(u.name,'') ILIKE $3) ORDER BY a.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date/time", "Action", "Module", "Record", "User", "IP"}
		case "offline-sync":
			q = `SELECT first_seen_at,request_id,terminal_id,status,attempts,COALESCE(invoice_no,''),COALESCE(last_error,''),COALESCE(resolution_reason,'') FROM pos_sync_transactions WHERE tenant_id=$4 AND first_seen_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (request_id ILIKE $3 OR terminal_id ILIKE $3 OR status ILIKE $3) ORDER BY last_attempt_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"First seen", "Transaction", "Terminal", "Status", "Attempts", "Invoice", "Error", "Resolution"}
		case "stock-transfers":
			q = `SELECT t.created_at::date,COALESCE(t.document_no,t.reference,''),fb.name,tb.name,p.name,t.quantity,t.dispatched_quantity,t.received_quantity,t.damaged_in_transit_quantity,t.status FROM stock_transfers t JOIN branches fb ON fb.id=t.from_branch_id JOIN branches tb ON tb.id=t.to_branch_id JOIN products p ON p.id=t.product_id WHERE t.tenant_id=$4 AND t.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (p.name ILIKE $3 OR COALESCE(t.document_no,t.reference,'') ILIKE $3 OR t.status ILIKE $3) ORDER BY t.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Document", "Source", "Destination", "Product", "Requested", "Dispatched", "Received", "Damaged", "Status"}
		case "unit-conversions":
			q = `SELECT e.created_at,p.sku,p.name,e.event_type,e.source_quantity,COALESCE(su.symbol,'base'),e.source_base_quantity,COALESCE(tp.name,'—'),e.target_quantity,COALESCE(tu.symbol,'base'),e.source_cost,e.target_unit_cost,e.yield_percent,e.reason,COALESCE(u.name,'System') FROM unit_conversion_events e JOIN products p ON p.id=e.source_product_id LEFT JOIN products tp ON tp.id=e.target_product_id LEFT JOIN units su ON su.id=e.source_unit_id LEFT JOIN units tu ON tu.id=e.target_unit_id LEFT JOIN users u ON u.id=e.created_by WHERE e.tenant_id=$4 AND e.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (p.name ILIKE $3 OR p.sku ILIKE $3 OR e.event_type ILIKE $3 OR e.reason ILIKE $3) ORDER BY e.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date/time", "SKU", "Source product", "Action", "Entered quantity", "Unit", "Base quantity", "Result product", "Result quantity", "Result unit", "Cost value", "Result unit cost", "Yield %", "Reason", "User"}
		case "cashier-reconciliation":
			q = `SELECT s.opened_at,COALESCE(s.closed_at::text,''),u.name,s.opening_cash,COALESCE(s.expected_cash,0),COALESCE(s.closing_cash,0),COALESCE(s.variance,0),CASE WHEN s.closed_at IS NULL THEN 'Open' WHEN s.reconciled_at IS NULL THEN 'Closed' ELSE 'Reconciled' END FROM cashier_sessions s JOIN users u ON u.id=s.user_id WHERE s.tenant_id=$4 AND s.opened_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND u.name ILIKE $3 ORDER BY s.opened_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Opened", "Closed", "Cashier", "Opening", "Expected", "Actual", "Variance", "Status"}
		case "login-history":
			q = `SELECT h.created_at,h.identifier,h.success,COALESCE(u.name,'Unknown'),COALESCE(h.ip_address::text,''),COALESCE(h.user_agent,'') FROM login_history h LEFT JOIN users u ON u.id=h.user_id WHERE h.tenant_id=$4 AND h.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (h.identifier ILIKE $3 OR COALESCE(u.name,'') ILIKE $3) ORDER BY h.created_at DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date/time", "Identifier", "Success", "User", "IP", "Device"}
		case "brand-sales":
			q = `SELECT COALESCE(br.name,'Unbranded'),sum(i.quantity),sum(i.quantity*i.unit_price-i.discount),sum(i.quantity*i.unit_cost),sum(i.quantity*(i.unit_price-i.unit_cost)-i.discount) FROM sale_items i JOIN sales s ON s.id=i.sale_id JOIN products p ON p.id=i.product_id LEFT JOIN brands br ON br.id=p.brand_id WHERE s.tenant_id=$4 AND s.status<>'cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND COALESCE(br.name,'Unbranded') ILIKE $3 GROUP BY br.id,br.name ORDER BY 3 DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Brand", "Quantity", "Net sales", "Cost", "Gross profit"}
		case "branch-sales":
			q = `SELECT COALESCE(b.name,'Primary'),count(*),sum(s.total-s.returned_total),sum(s.paid_amount-s.returned_paid),sum(s.balance-s.returned_receivable) FROM sales s LEFT JOIN branches b ON b.id=s.branch_id WHERE s.tenant_id=$4 AND s.status<>'cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND COALESCE(b.name,'Primary') ILIKE $3 GROUP BY b.id,b.name ORDER BY 3 DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Branch", "Invoices", "Net sales", "Collected", "Receivable"}
		case "hourly-sales":
			q = `SELECT to_char(s.created_at,'HH24:00'),count(*),sum(s.total-s.returned_total),avg(s.total-s.returned_total) FROM sales s WHERE s.tenant_id=$4 AND s.status<>'cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND to_char(s.created_at,'HH24:00') ILIKE $3 GROUP BY 1 ORDER BY 1`
			args = []any{from, to, search, tenant}
			headers = []string{"Hour", "Invoices", "Net sales", "Average basket"}
		case "discount-analysis":
			q = `SELECT s.created_at::date,s.invoice_no,COALESCE(u.name,'Unknown'),s.total,s.discount,CASE WHEN s.total+s.discount=0 THEN 0 ELSE round(s.discount*100/(s.total+s.discount),2) END FROM sales s LEFT JOIN users u ON u.id=s.cashier_id WHERE s.tenant_id=$4 AND s.discount>0 AND s.status<>'cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (s.invoice_no ILIKE $3 OR COALESCE(u.name,'') ILIKE $3) ORDER BY s.discount DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Invoice", "Cashier", "Net total", "Discount", "Discount %"}
		case "product-profit":
			q = `SELECT p.sku,p.name,sum(i.quantity),sum(i.quantity*i.unit_price-i.discount),sum(i.quantity*i.unit_cost),sum(i.quantity*(i.unit_price-i.unit_cost)-i.discount) FROM sale_items i JOIN sales s ON s.id=i.sale_id JOIN products p ON p.id=i.product_id WHERE s.tenant_id=$4 AND s.status<>'cancelled' AND s.created_at::date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (p.name ILIKE $3 OR p.sku ILIKE $3) GROUP BY p.id ORDER BY 6 DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"SKU", "Product", "Quantity", "Net sales", "COGS", "Gross profit"}
		case "product-purchases":
			q = `SELECT pr.sku,pr.name,sum(i.quantity+i.free_quantity),sum(i.total),avg(i.unit_cost),max(p.purchase_date) FROM purchase_items i JOIN purchases p ON p.id=i.purchase_id JOIN products pr ON pr.id=i.product_id WHERE p.tenant_id=$4 AND p.status='finalized' AND p.purchase_date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (pr.name ILIKE $3 OR pr.sku ILIKE $3) GROUP BY pr.id ORDER BY 4 DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"SKU", "Product", "Received quantity", "Purchase value", "Average cost", "Last purchase"}
		case "purchase-price-variance":
			q = `SELECT p.purchase_date,p.invoice_no,pr.sku,pr.name,i.unit_cost,pr.purchase_price,i.unit_cost-pr.purchase_price FROM purchase_items i JOIN purchases p ON p.id=i.purchase_id JOIN products pr ON pr.id=i.product_id WHERE p.tenant_id=$4 AND p.status='finalized' AND p.purchase_date BETWEEN COALESCE(NULLIF($1,'')::date,'2000-01-01') AND COALESCE(NULLIF($2,'')::date,current_date) AND (pr.name ILIKE $3 OR pr.sku ILIKE $3 OR p.invoice_no ILIKE $3) ORDER BY abs(i.unit_cost-pr.purchase_price) DESC`
			args = []any{from, to, search, tenant}
			headers = []string{"Date", "Invoice", "SKU", "Product", "GRN cost", "Current cost", "Variance"}
		case "stock-valuation-category":
			q = `SELECT COALESCE(c.name,'Uncategorised'),count(*),sum(p.stock_quantity),sum(p.stock_quantity*p.purchase_price),sum(p.stock_quantity*p.selling_price) FROM products p LEFT JOIN categories c ON c.id=p.category_id WHERE p.tenant_id=$2 AND p.is_active AND COALESCE(c.name,'Uncategorised') ILIKE $1 GROUP BY c.id,c.name ORDER BY 4 DESC`
			args = []any{search, tenant}
			headers = []string{"Category", "Products", "Quantity", "Cost value", "Retail value"}
		case "batch-stock":
			q = `SELECT p.sku,p.name,b.batch_no,b.manufactured_date,b.expiry_date,b.available_qty,b.unit_cost,b.available_qty*b.unit_cost FROM stock_batches b JOIN products p ON p.id=b.product_id WHERE p.tenant_id=$2 AND b.available_qty>0 AND (p.name ILIKE $1 OR p.sku ILIKE $1 OR b.batch_no ILIKE $1) ORDER BY b.expiry_date NULLS LAST`
			args = []any{search, tenant}
			headers = []string{"SKU", "Product", "Batch", "Manufactured", "Expiry", "Quantity", "Unit cost", "Value"}
		case "slow-moving-stock":
			q = `SELECT p.sku,p.name,p.stock_quantity,p.purchase_price,COALESCE(max(s.created_at)::date::text,'Never sold'),COALESCE(sum(i.quantity) FILTER(WHERE s.created_at>=current_date-90),0) FROM products p LEFT JOIN sale_items i ON i.product_id=p.id LEFT JOIN sales s ON s.id=i.sale_id AND s.status<>'cancelled' WHERE p.tenant_id=$2 AND p.is_active AND (p.name ILIKE $1 OR p.sku ILIKE $1) GROUP BY p.id HAVING COALESCE(sum(i.quantity) FILTER(WHERE s.created_at>=current_date-90),0)<=5 ORDER BY 6,p.stock_quantity DESC`
			args = []any{search, tenant}
			headers = []string{"SKU", "Product", "Stock", "Cost", "Last sold", "90-day quantity"}
		case "credit-utilization":
			q = `SELECT name,phone,credit_limit,balance,CASE WHEN credit_limit=0 THEN 0 ELSE round(balance*100/credit_limit,2) END,CASE WHEN balance>credit_limit AND credit_limit>0 THEN 'Over limit' ELSE 'Within limit' END FROM customers WHERE tenant_id=$2 AND (name ILIKE $1 OR phone ILIKE $1) ORDER BY 5 DESC`
			args = []any{search, tenant}
			headers = []string{"Customer", "Phone", "Credit limit", "Outstanding", "Used %", "Status"}
		case "comparative-profit-loss":
			q = `SELECT a.account_type,a.code,a.name,sum(CASE WHEN je.entry_date BETWEEN COALESCE(NULLIF($1,'')::date,current_date-30) AND COALESCE(NULLIF($2,'')::date,current_date) THEN CASE WHEN a.account_type='income' THEN jl.credit-jl.debit ELSE jl.debit-jl.credit END ELSE 0 END),sum(CASE WHEN je.entry_date BETWEEN (COALESCE(NULLIF($1,'')::date,current_date-30)-(COALESCE(NULLIF($2,'')::date,current_date)-COALESCE(NULLIF($1,'')::date,current_date-30)+1)) AND (COALESCE(NULLIF($1,'')::date,current_date-30)-1) THEN CASE WHEN a.account_type='income' THEN jl.credit-jl.debit ELSE jl.debit-jl.credit END ELSE 0 END) FROM chart_of_accounts a JOIN journal_lines jl ON jl.account_id=a.id JOIN journal_entries je ON je.id=jl.journal_id WHERE a.tenant_id=$4 AND je.status='posted' AND a.account_type IN('income','expense') AND (a.code ILIKE $3 OR a.name ILIKE $3) GROUP BY a.id ORDER BY a.account_type,a.code`
			args = []any{from, to, search, tenant}
			headers = []string{"Type", "Code", "Account", "Selected period", "Previous period"}
		default:
			http.Error(w, "unknown report", 404)
			return
		}
		rows, e := db.Query(r.Context(), q, args...)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		data := [][]string{}
		for rows.Next() {
			v, e := rows.Values()
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			line := make([]string, len(v))
			for i, x := range v {
				line[i] = strings.TrimSpace(strings.ReplaceAll(strings.Trim(strings.TrimSpace(toText(x)), "{}"), "\n", " "))
			}
			data = append(data, line)
		}
		if r.URL.Query().Get("format") == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", "attachment; filename="+name+"-report.csv")
			c := csv.NewWriter(w)
			c.Write(headers)
			c.WriteAll(data)
			return
		}
		if r.URL.Query().Get("format") == "xlsx" {
			book, e := createXLSX(headers, data)
			if e != nil {
				http.Error(w, "spreadsheet generation failed", 500)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			w.Header().Set("Content-Disposition", "attachment; filename="+name+"-report.xlsx")
			_, _ = w.Write(book)
			return
		}
		if r.URL.Query().Get("format") == "pdf" {
			document, e := createPDF(name, headers, data)
			if e != nil {
				http.Error(w, "PDF generation failed", 500)
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", "attachment; filename="+name+"-report.pdf")
			_, _ = w.Write(document)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"name": name, "headers": headers, "rows": data, "count": len(data)})
	}
}

func xmlText(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, value)
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

func excelColumn(index int) string {
	name := ""
	for index >= 0 {
		name = string(rune('A'+index%26)) + name
		index = index/26 - 1
	}
	return name
}

func createXLSX(headers []string, data [][]string) ([]byte, error) {
	var output bytes.Buffer
	book := zip.NewWriter(&output)
	files := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Report" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"xl/styles.xml":              `<?xml version="1.0" encoding="UTF-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><color rgb="FFFFFFFF"/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF5064DC"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/></cellXfs></styleSheet>`,
	}
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols>`)
	for i := range headers {
		fmt.Fprintf(&sheet, `<col min="%d" max="%d" width="20" customWidth="1"/>`, i+1, i+1)
	}
	sheet.WriteString(`</cols><sheetData><row r="1">`)
	for i, value := range headers {
		fmt.Fprintf(&sheet, `<c r="%s1" t="inlineStr" s="1"><is><t>%s</t></is></c>`, excelColumn(i), xmlText(value))
	}
	sheet.WriteString(`</row>`)
	for rowIndex, row := range data {
		fmt.Fprintf(&sheet, `<row r="%d">`, rowIndex+2)
		for column, value := range row {
			fmt.Fprintf(&sheet, `<c r="%s%d" t="inlineStr"><is><t>%s</t></is></c>`, excelColumn(column), rowIndex+2, xmlText(value))
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData>`)
	if len(headers) > 0 {
		fmt.Fprintf(&sheet, `<autoFilter ref="A1:%s%d"/>`, excelColumn(len(headers)-1), len(data)+1)
	}
	sheet.WriteString(`</worksheet>`)
	files["xl/worksheets/sheet1.xml"] = sheet.String()
	for name, content := range files {
		writer, e := book.Create(name)
		if e != nil {
			return nil, e
		}
		if _, e = writer.Write([]byte(content)); e != nil {
			return nil, e
		}
	}
	if e := book.Close(); e != nil {
		return nil, e
	}
	return output.Bytes(), nil
}
func toText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	case float64:
		return strconv.FormatFloat(x, 'f', 2, 64)
	case pgtype.Numeric:
		f, e := x.Float64Value()
		if e == nil && f.Valid {
			return strconv.FormatFloat(f.Float64, 'f', 2, 64)
		}
		return "0"
	default:
		return fmt.Sprint(x)
	}
}
