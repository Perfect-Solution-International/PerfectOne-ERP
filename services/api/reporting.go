package main

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func registerReports(m *http.ServeMux, db *pgxpool.Pool) {
	m.HandleFunc("GET /dashboard", jsonAPI(authenticated(db, allStaff...)(dashboard(db))))
	m.HandleFunc("GET /sales", jsonAPI(authenticated(db)(sales(db))))
	m.HandleFunc("GET /low-stock", jsonAPI(authenticated(db, allStaff...)(lowStock(db))))
	m.HandleFunc("GET /reports/summary", jsonAPI(authenticated(db, financeRoles...)(summary(db))))
}
func summary(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := claimsFrom(r).Tenant
		var sales, purchases, expenses, receivables, payables float64
		db.QueryRow(r.Context(), `SELECT COALESCE((SELECT sum(total-returned_total) FROM sales WHERE tenant_id=$1 AND status<>'cancelled'),0),COALESCE((SELECT sum(total) FROM purchases WHERE tenant_id=$1 AND status='finalized'),0)-COALESCE((SELECT sum(total) FROM purchase_returns WHERE tenant_id=$1 AND status='finalized'),0),COALESCE((SELECT sum(jl.debit-jl.credit) FROM journal_lines jl JOIN journal_entries je ON je.id=jl.journal_id JOIN chart_of_accounts a ON a.id=jl.account_id WHERE je.tenant_id=$1 AND je.status='posted' AND a.account_type='expense'),0),COALESCE((SELECT sum(balance) FROM customers WHERE tenant_id=$1),0),COALESCE((SELECT sum(balance) FROM suppliers WHERE tenant_id=$1),0)`, tenant).Scan(&sales, &purchases, &expenses, &receivables, &payables)
		var profit float64
		db.QueryRow(r.Context(), `SELECT COALESCE(sum(CASE WHEN a.account_type='income' THEN jl.credit-jl.debit WHEN a.account_type='expense' THEN jl.credit-jl.debit ELSE 0 END),0) FROM journal_lines jl JOIN journal_entries je ON je.id=jl.journal_id JOIN chart_of_accounts a ON a.id=jl.account_id WHERE je.tenant_id=$1 AND je.status='posted'`, tenant).Scan(&profit)
		json.NewEncoder(w).Encode(map[string]any{"totalSales": sales, "totalPurchases": purchases, "expenses": expenses, "grossProfit": profit, "receivables": receivables, "payables": payables})
	}
}
func dashboardRange(r *http.Request) (time.Time, time.Time, string) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "today"
	}
	start, end := today, today.AddDate(0, 0, 1)
	switch period {
	case "yesterday":
		start = today.AddDate(0, 0, -1)
		end = today
	case "week":
		start = today.AddDate(0, 0, -int(today.Weekday())+1)
		if today.Weekday() == time.Sunday {
			start = today.AddDate(0, 0, -6)
		}
		end = today.AddDate(0, 0, 1)
	case "month":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		end = start.AddDate(0, 1, 0)
	case "year":
		start = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		end = start.AddDate(1, 0, 0)
	case "custom":
		from, e1 := time.Parse("2006-01-02", r.URL.Query().Get("from"))
		to, e2 := time.Parse("2006-01-02", r.URL.Query().Get("to"))
		if e1 == nil && e2 == nil && to.Before(from) == false {
			start = from
			end = to.AddDate(0, 0, 1)
		}
	}
	return start, end, period
}
func hasAny(perms []string, keys ...string) bool {
	for _, p := range perms {
		for _, k := range keys {
			if p == k {
				return true
			}
		}
	}
	return false
}
func dashboard(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		start, end, period := dashboardRange(r)
		perms, e := effectivePermissions(r.Context(), db, c.Sub, c.Role)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		showSales := hasAny(perms, "dashboard", "sales", "sales.view", "pos")
		showPurchases := hasAny(perms, "purchases", "purchases.view")
		showStock := hasAny(perms, "inventory", "inventory.view", "products", "products.view")
		showFinance := hasAny(perms, "accounting", "accounting.view", "financial_reports", "reports", "reports.view")
		showContacts := hasAny(perms, "contacts", "customers.view", "suppliers.view", "accounting")
		metrics := map[string]any{}
		var todaySales, todayPurchases, monthSales, monthPurchases float64
		if showSales {
			db.QueryRow(r.Context(), `SELECT COALESCE(sum(total-returned_total),0) FROM sales WHERE tenant_id=$1 AND status<>'cancelled' AND created_at::date=current_date`, c.Tenant).Scan(&todaySales)
			db.QueryRow(r.Context(), `SELECT COALESCE(sum(total-returned_total),0) FROM sales WHERE tenant_id=$1 AND status<>'cancelled' AND date_trunc('month',created_at)=date_trunc('month',current_date)`, c.Tenant).Scan(&monthSales)
			metrics["todaySales"] = todaySales
			metrics["monthlySales"] = monthSales
		}
		if showPurchases {
			db.QueryRow(r.Context(), `SELECT COALESCE(sum(total),0) FROM purchases WHERE tenant_id=$1 AND status='finalized' AND purchase_date=current_date`, c.Tenant).Scan(&todayPurchases)
			db.QueryRow(r.Context(), `SELECT COALESCE(sum(total),0) FROM purchases WHERE tenant_id=$1 AND status='finalized' AND date_trunc('month',purchase_date)=date_trunc('month',current_date)`, c.Tenant).Scan(&monthPurchases)
			metrics["todayPurchases"] = todayPurchases
			metrics["monthlyPurchases"] = monthPurchases
		}
		if showStock {
			var stock float64
			var low, out, near, expired int
			var damaged float64
			db.QueryRow(r.Context(), `SELECT COALESCE(sum(stock_quantity*purchase_price),0),count(*) FILTER(WHERE stock_quantity>0 AND stock_quantity<=minimum_stock),count(*) FILTER(WHERE stock_quantity<=0) FROM products WHERE tenant_id=$1 AND is_active`, c.Tenant).Scan(&stock, &low, &out)
			db.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE available_qty>0 AND expiry_date BETWEEN current_date AND current_date+30),count(*) FILTER(WHERE available_qty>0 AND expiry_date<current_date) FROM stock_batches b JOIN products p ON p.id=b.product_id WHERE p.tenant_id=$1`, c.Tenant).Scan(&near, &expired)
			db.QueryRow(r.Context(), `SELECT COALESCE(sum(d.quantity),0) FROM damaged_items d WHERE d.tenant_id=$1 AND d.status='finalized' AND d.finalized_at>=$2 AND d.finalized_at<$3`, c.Tenant, start, end).Scan(&damaged)
			metrics["stockValue"] = stock
			metrics["lowStock"] = low
			metrics["outOfStock"] = out
			metrics["nearExpiry"] = near
			metrics["expired"] = expired
			metrics["damagedItems"] = damaged
		}
		if showContacts {
			var receivables, payables float64
			db.QueryRow(r.Context(), `SELECT COALESCE((SELECT sum(balance) FROM customers WHERE tenant_id=$1 AND is_active),0),COALESCE((SELECT sum(balance) FROM suppliers WHERE tenant_id=$1 AND is_active),0)`, c.Tenant).Scan(&receivables, &payables)
			metrics["customerReceivables"] = receivables
			metrics["supplierPayables"] = payables
		}
		if showFinance {
			var cash, bank, expenses, revenue, cost float64
			db.QueryRow(r.Context(), `SELECT COALESCE(sum(CASE WHEN account_type='cash' THEN balance ELSE 0 END),0),COALESCE(sum(CASE WHEN account_type='bank' THEN balance ELSE 0 END),0) FROM(SELECT a.account_type,a.opening_balance+COALESCE(sum(CASE WHEN t.transaction_type IN('deposit','income','transfer_in') THEN t.amount ELSE -t.amount END),0) balance FROM cash_accounts a LEFT JOIN cash_transactions t ON t.account_id=a.id WHERE a.tenant_id=$1 AND a.is_active GROUP BY a.id)a`, c.Tenant).Scan(&cash, &bank)
			db.QueryRow(r.Context(), `SELECT COALESCE(sum(CASE WHEN a.account_type='expense' THEN jl.debit-jl.credit ELSE 0 END),0),COALESCE(sum(CASE WHEN a.account_type='income' THEN jl.credit-jl.debit ELSE 0 END),0),COALESCE(sum(CASE WHEN a.code='5000' THEN jl.debit-jl.credit ELSE 0 END),0) FROM journal_lines jl JOIN journal_entries je ON je.id=jl.journal_id JOIN chart_of_accounts a ON a.id=jl.account_id WHERE je.tenant_id=$1 AND je.status='posted' AND je.entry_date>=$2::date AND je.entry_date<$3::date`, c.Tenant, start, end).Scan(&expenses, &revenue, &cost)
			metrics["cashBalance"] = cash
			metrics["bankBalance"] = bank
			metrics["expenses"] = expenses - cost
			metrics["profit"] = revenue - expenses
		}
		cashiers := []map[string]any{}
		if showSales && hasAny(perms, "reports", "reports.view", "sales.view") {
			rows, e := db.Query(r.Context(), `SELECT u.name,count(s.id),COALESCE(sum(s.total-s.returned_total),0),COALESCE(avg(s.total-s.returned_total),0) FROM sales s JOIN users u ON u.id=s.cashier_id WHERE s.tenant_id=$1 AND s.status<>'cancelled' AND s.created_at>=$2 AND s.created_at<$3 GROUP BY u.id ORDER BY sum(s.total-s.returned_total) DESC LIMIT 8`, c.Tenant, start, end)
			if e == nil {
				defer rows.Close()
				for rows.Next() {
					var name string
					var count int
					var total, average float64
					rows.Scan(&name, &count, &total, &average)
					cashiers = append(cashiers, map[string]any{"name": name, "sales": count, "total": total, "average": average})
				}
			}
		}
		products := []map[string]any{}
		if showSales {
			rows, e := db.Query(r.Context(), `SELECT p.name,sum(si.quantity-COALESCE((SELECT sum(ri.quantity) FROM sale_return_items ri JOIN sale_returns rr ON rr.id=ri.return_id WHERE ri.sale_item_id=si.id AND rr.status='finalized'),0)),sum(si.total-COALESCE((SELECT sum(ri.total) FROM sale_return_items ri JOIN sale_returns rr ON rr.id=ri.return_id WHERE ri.sale_item_id=si.id AND rr.status='finalized'),0)) FROM sale_items si JOIN sales s ON s.id=si.sale_id JOIN products p ON p.id=si.product_id WHERE s.tenant_id=$1 AND s.status<>'cancelled' AND s.created_at>=$2 AND s.created_at<$3 GROUP BY p.id ORDER BY 2 DESC LIMIT 8`, c.Tenant, start, end)
			if e == nil {
				defer rows.Close()
				for rows.Next() {
					var name string
					var quantity, total float64
					rows.Scan(&name, &quantity, &total)
					products = append(products, map[string]any{"name": name, "quantity": quantity, "total": total})
				}
			}
		}
		insights:=map[string]any{"role":c.Role};var invoices int;var netSales,discounts,returns,previousSales float64
		if showSales{duration:=end.Sub(start);previousStart:=start.Add(-duration);_ = db.QueryRow(r.Context(),`SELECT count(*),COALESCE(sum(total-returned_total),0),COALESCE(sum(discount),0),COALESCE(sum(returned_total),0) FROM sales WHERE tenant_id=$1 AND status<>'cancelled' AND created_at>=$2 AND created_at<$3`,c.Tenant,start,end).Scan(&invoices,&netSales,&discounts,&returns);_ = db.QueryRow(r.Context(),`SELECT COALESCE(sum(total-returned_total),0) FROM sales WHERE tenant_id=$1 AND status<>'cancelled' AND created_at>=$2 AND created_at<$3`,c.Tenant,previousStart,start).Scan(&previousSales);metrics["invoiceCount"]=invoices;metrics["averageBasket"]=0.0;if invoices>0{metrics["averageBasket"]=netSales/float64(invoices)};insights["salesChange"]=0.0;if previousSales!=0{insights["salesChange"]=(netSales-previousSales)*100/previousSales};insights["discounts"]=discounts;insights["returns"]=returns}
		alerts:=[]map[string]any{};if showStock{for _,a:=range []struct{title,kind,href string;value any}{{"Low stock needs attention","warning","/inventory?section=low-stock",metrics["lowStock"]},{"Expired stock requires action","danger","/expiry",metrics["expired"]},{"Out-of-stock products","danger","/inventory?section=stock",metrics["outOfStock"]}}{alerts=append(alerts,map[string]any{"title":a.title,"kind":a.kind,"href":a.href,"value":a.value})}}
		if hasAny(perms,"purchases","purchases.view"){var draft,partial,overdue int;_ = db.QueryRow(r.Context(),`SELECT count(*) FILTER(WHERE status IN('draft','approved','ordered')),count(*) FILTER(WHERE status='partially_received'),count(*) FILTER(WHERE expected_date<current_date AND status NOT IN('completed','cancelled')) FROM purchase_orders WHERE tenant_id=$1`,c.Tenant).Scan(&draft,&partial,&overdue);insights["procurement"]=map[string]any{"pending":draft,"partial":partial,"overdue":overdue}}
		branches:=[]map[string]any{};if showSales{rows,e:=db.Query(r.Context(),`SELECT COALESCE(b.name,'Primary'),count(s.id),COALESCE(sum(s.total-s.returned_total),0) FROM sales s LEFT JOIN branches b ON b.id=s.branch_id WHERE s.tenant_id=$1 AND s.status<>'cancelled' AND s.created_at>=$2 AND s.created_at<$3 GROUP BY b.id,b.name ORDER BY 3 DESC LIMIT 8`,c.Tenant,start,end);if e==nil{defer rows.Close();for rows.Next(){var n string;var count int;var total float64;if rows.Scan(&n,&count,&total)==nil{branches=append(branches,map[string]any{"name":n,"count":count,"total":total})}}}}
		activity:=[]map[string]any{};if c.Role=="super_admin"||c.Role=="admin"||c.Role=="manager"{rows,e:=db.Query(r.Context(),`SELECT a.action,a.entity_type,COALESCE(u.name,'System'),a.created_at FROM audit_logs a LEFT JOIN users u ON u.id=a.user_id WHERE a.tenant_id=$1 ORDER BY a.created_at DESC LIMIT 8`,c.Tenant);if e==nil{defer rows.Close();for rows.Next(){var action,module,user string;var at time.Time;if rows.Scan(&action,&module,&user,&at)==nil{activity=append(activity,map[string]any{"action":action,"module":module,"user":user,"at":at})}}}}
		json.NewEncoder(w).Encode(map[string]any{"period": period, "from": start.Format("2006-01-02"), "to": end.AddDate(0, 0, -1).Format("2006-01-02"), "metrics": metrics, "cashierPerformance": cashiers, "bestSellingProducts": products,"insights":insights,"alerts":alerts,"branchPerformance":branches,"activity":activity})
	}
}
func sales(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		permissions, err := effectivePermissions(r.Context(), db, c.Sub, c.Role)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		viewAll := c.Role == "super_admin" || c.Role == "admin" || hasAny(permissions, "sales", "sales.view")
		viewOwn := viewAll || hasAny(permissions, "own_sales", "own_sales.view")
		if !viewOwn {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if page < 1 {
			page = 1
		}
		if limit != 25 && limit != 50 && limit != 100 {
			limit = 25
		}
		where := []string{"s.tenant_id=$1"}
		args := []any{c.Tenant}
		add := func(condition string, value any) {
			args = append(args, value)
			where = append(where, fmt.Sprintf(condition, len(args)))
		}
		if !viewAll {
			add("s.cashier_id=$%d", c.Sub)
		}
		if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
			args = append(args, q)
			index := len(args)
			where = append(where, fmt.Sprintf("(s.invoice_no ILIKE '%%'||$%d||'%%' OR COALESCE(cu.name,'') ILIKE '%%'||$%d||'%%')", index, index))
		}
		if from := r.URL.Query().Get("from"); from != "" {
			add("s.created_at::date >= $%d::date", from)
		}
		if to := r.URL.Query().Get("to"); to != "" {
			add("s.created_at::date <= $%d::date", to)
		}
		if customer := r.URL.Query().Get("customerId"); customer != "" {
			add("s.customer_id=$%d::uuid", customer)
		}
		if cashier := r.URL.Query().Get("cashierId"); cashier != "" && viewAll {
			add("s.cashier_id=$%d::uuid", cashier)
		}
		if method := r.URL.Query().Get("paymentMethod"); method == "cash" || method == "bank" || method == "card" || method == "credit" {
			add("s.payment_method=$%d", method)
		}
		if status := r.URL.Query().Get("status"); status == "has_returns" {
			where = append(where, "s.returned_total>0")
		} else if status == "completed" || status == "partial_returned" || status == "returned" || status == "cancelled" {
			add("s.status=$%d", status)
		}
		clause := strings.Join(where, " AND ")
		fromSQL := ` FROM sales s LEFT JOIN customers cu ON cu.id=s.customer_id LEFT JOIN users u ON u.id=s.cashier_id LEFT JOIN branches b ON b.id=s.branch_id WHERE ` + clause
		var totalRows int
		var grossTotal, netTotal, paidTotal, balanceTotal float64
		err = db.QueryRow(r.Context(), `SELECT count(*),COALESCE(sum(s.total),0),COALESCE(sum(CASE WHEN s.status='cancelled' THEN 0 ELSE s.total-s.returned_total END),0),COALESCE(sum(CASE WHEN s.status='cancelled' THEN 0 ELSE s.paid_amount-s.returned_paid END),0),COALESCE(sum(CASE WHEN s.status='cancelled' THEN 0 ELSE s.balance-s.returned_receivable END),0)`+fromSQL, args...).Scan(&totalRows, &grossTotal, &netTotal, &paidTotal, &balanceTotal)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		queryArgs := append(append([]any{}, args...), limit, (page-1)*limit)
		rows, err := db.Query(r.Context(), `SELECT s.id,s.invoice_no,COALESCE(cu.name,'Walk-in customer'),COALESCE(u.name,'Unknown user'),COALESCE(b.name,''),s.total,s.returned_total,s.paid_amount,s.returned_paid,s.balance,s.returned_receivable,s.discount,s.payment_method,s.status,s.created_at`+fromSQL+fmt.Sprintf(` ORDER BY s.created_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2), queryArgs...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, invoice, customer, cashier, branch, method, status string
			var total, returned, paid, returnedPaid, balance, returnedReceivable, discount float64
			var created any
			if err = rows.Scan(&id, &invoice, &customer, &cashier, &branch, &total, &returned, &paid, &returnedPaid, &balance, &returnedReceivable, &discount, &method, &status, &created); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			out = append(out, map[string]any{"id": id, "invoice": invoice, "customer": customer, "cashier": cashier, "branch": branch, "total": total, "returned": returned, "netTotal": total - returned, "paid": paid, "netPaid": paid - returnedPaid, "balance": balance - returnedReceivable, "discount": discount, "paymentMethod": method, "status": status, "createdAt": created})
		}
		cashiers := []map[string]string{}
		if viewAll {
			cashierRows, queryErr := db.Query(r.Context(), `SELECT DISTINCT u.id,u.name FROM users u JOIN sales s ON s.cashier_id=u.id WHERE s.tenant_id=$1 ORDER BY u.name`, c.Tenant)
			if queryErr == nil {
				defer cashierRows.Close()
				for cashierRows.Next() {
					var id, name string
					if cashierRows.Scan(&id, &name) == nil {
						cashiers = append(cashiers, map[string]string{"id": id, "name": name})
					}
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"rows": out, "total": totalRows, "page": page, "limit": limit, "canViewAll": viewAll, "cashiers": cashiers, "summary": map[string]any{"gross": grossTotal, "net": netTotal, "paid": paidTotal, "balance": balanceTotal}})
	}
}
func lowStock(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, e := db.Query(r.Context(), `SELECT id,name,sku,stock_quantity,minimum_stock FROM products WHERE is_active AND stock_quantity<=minimum_stock AND tenant_id=$1 ORDER BY stock_quantity`, claimsFrom(r).Tenant)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, sku string
			var stock, min float64
			rows.Scan(&id, &name, &sku, &stock, &min)
			out = append(out, map[string]any{"id": id, "name": name, "sku": sku, "stock": stock, "minimum": min})
		}
		json.NewEncoder(w).Encode(out)
	}
}
