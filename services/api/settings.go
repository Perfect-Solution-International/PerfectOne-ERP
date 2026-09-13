package main

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

type tenantSettings struct {
	BusinessName         string  `json:"businessName"`
	LogoURL              string  `json:"logoUrl"`
	Address              string  `json:"address"`
	Phone                string  `json:"phone"`
	Email                string  `json:"email"`
	CurrencyCode         string  `json:"currencyCode"`
	CurrencySymbol       string  `json:"currencySymbol"`
	DefaultTaxRate       float64 `json:"defaultTaxRate"`
	InvoicePrefix        string  `json:"invoicePrefix"`
	ReceiptHeader        string  `json:"receiptHeader"`
	ReceiptFooter        string  `json:"receiptFooter"`
	ReceiptSize          string  `json:"receiptSize"`
	ShowTax              bool    `json:"showTax"`
	ShowDiscount         bool    `json:"showDiscount"`
	LowStockEnabled      bool    `json:"lowStockEnabled"`
	ExpiryAlertDays      int     `json:"expiryAlertDays"`
	DefaultPaymentMethod string  `json:"defaultPaymentMethod"`
	BusinessRegistrationNo string `json:"businessRegistrationNo"`
	TaxRegistrationNo string `json:"taxRegistrationNo"`
	InvoiceNumberDigits int `json:"invoiceNumberDigits"`
	ReceiptTitle string `json:"receiptTitle"`
	ShowBusinessLogo bool `json:"showBusinessLogo"`
	ShowBusinessAddress bool `json:"showBusinessAddress"`
	ShowBusinessContact bool `json:"showBusinessContact"`
	ReceiptCopies int `json:"receiptCopies"`
	UpdatedAt            any     `json:"updatedAt"`
}

func registerSettings(mux *http.ServeMux, db *pgxpool.Pool) {
	mux.HandleFunc("GET /settings", jsonAPI(authenticated(db, allStaff...)(getSettings(db))))
	mux.HandleFunc("PUT /settings", jsonAPI(authenticated(db, "super_admin", "admin")(saveSettings(db))))
}

func getSettings(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		var x tenantSettings
		e := db.QueryRow(r.Context(), `INSERT INTO tenant_settings(tenant_id,business_name) SELECT $1,name FROM tenants WHERE id=$1 ON CONFLICT(tenant_id) DO UPDATE SET tenant_id=EXCLUDED.tenant_id RETURNING business_name,COALESCE(logo_url,''),COALESCE(address,''),COALESCE(phone,''),COALESCE(email,''),currency_code,currency_symbol,default_tax_rate,invoice_prefix,COALESCE(receipt_header,''),COALESCE(receipt_footer,''),receipt_size,show_tax,show_discount,low_stock_enabled,expiry_alert_days,default_payment_method,COALESCE(business_registration_no,''),COALESCE(tax_registration_no,''),invoice_number_digits,receipt_title,show_business_logo,show_business_address,show_business_contact,receipt_copies,updated_at`, c.Tenant).Scan(&x.BusinessName, &x.LogoURL, &x.Address, &x.Phone, &x.Email, &x.CurrencyCode, &x.CurrencySymbol, &x.DefaultTaxRate, &x.InvoicePrefix, &x.ReceiptHeader, &x.ReceiptFooter, &x.ReceiptSize, &x.ShowTax, &x.ShowDiscount, &x.LowStockEnabled, &x.ExpiryAlertDays, &x.DefaultPaymentMethod,&x.BusinessRegistrationNo,&x.TaxRegistrationNo,&x.InvoiceNumberDigits,&x.ReceiptTitle,&x.ShowBusinessLogo,&x.ShowBusinessAddress,&x.ShowBusinessContact,&x.ReceiptCopies, &x.UpdatedAt)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(x)
	}
}

func saveSettings(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var x tenantSettings
		if json.NewDecoder(r.Body).Decode(&x) != nil || strings.TrimSpace(x.BusinessName) == "" || strings.TrimSpace(x.CurrencyCode) == "" || strings.TrimSpace(x.InvoicePrefix) == "" || strings.TrimSpace(x.ReceiptTitle)=="" || x.DefaultTaxRate < 0 || x.ExpiryAlertDays < 0 || x.InvoiceNumberDigits<4 || x.InvoiceNumberDigits>12 || x.ReceiptCopies<1 || x.ReceiptCopies>3 {
			http.Error(w, "business name, currency and invoice prefix are required", 400)
			return
		}
		validSize := x.ReceiptSize == "58mm" || x.ReceiptSize == "80mm" || x.ReceiptSize == "A4"
		validMethod := x.DefaultPaymentMethod == "cash" || x.DefaultPaymentMethod == "bank" || x.DefaultPaymentMethod == "card" || x.DefaultPaymentMethod == "credit" || x.DefaultPaymentMethod == "cheque"
		if !validSize || !validMethod {
			http.Error(w, "invalid receipt size or payment method", 400)
			return
		}
		if len(strings.TrimSpace(x.CurrencyCode))!=3 || len(strings.TrimSpace(x.InvoicePrefix))>12 || x.DefaultTaxRate>100 { http.Error(w,"currency must be 3 letters, invoice prefix at most 12 characters, and tax at most 100%",400);return }
		c := claimsFrom(r)
		_, e := db.Exec(r.Context(), `INSERT INTO tenant_settings(tenant_id,business_name,logo_url,address,phone,email,currency_code,currency_symbol,default_tax_rate,invoice_prefix,receipt_header,receipt_footer,receipt_size,show_tax,show_discount,low_stock_enabled,expiry_alert_days,default_payment_method,business_registration_no,tax_registration_no,invoice_number_digits,receipt_title,show_business_logo,show_business_address,show_business_contact,receipt_copies,updated_by,updated_at)VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF(lower($6),''),upper($7),$8,$9,upper($10),NULLIF($11,''),NULLIF($12,''),$13,$14,$15,$16,$17,$18,NULLIF($19,''),NULLIF($20,''),$21,$22,$23,$24,$25,$26,NULLIF($27,'')::uuid,now())ON CONFLICT(tenant_id)DO UPDATE SET business_name=EXCLUDED.business_name,logo_url=EXCLUDED.logo_url,address=EXCLUDED.address,phone=EXCLUDED.phone,email=EXCLUDED.email,currency_code=EXCLUDED.currency_code,currency_symbol=EXCLUDED.currency_symbol,default_tax_rate=EXCLUDED.default_tax_rate,invoice_prefix=EXCLUDED.invoice_prefix,receipt_header=EXCLUDED.receipt_header,receipt_footer=EXCLUDED.receipt_footer,receipt_size=EXCLUDED.receipt_size,show_tax=EXCLUDED.show_tax,show_discount=EXCLUDED.show_discount,low_stock_enabled=EXCLUDED.low_stock_enabled,expiry_alert_days=EXCLUDED.expiry_alert_days,default_payment_method=EXCLUDED.default_payment_method,business_registration_no=EXCLUDED.business_registration_no,tax_registration_no=EXCLUDED.tax_registration_no,invoice_number_digits=EXCLUDED.invoice_number_digits,receipt_title=EXCLUDED.receipt_title,show_business_logo=EXCLUDED.show_business_logo,show_business_address=EXCLUDED.show_business_address,show_business_contact=EXCLUDED.show_business_contact,receipt_copies=EXCLUDED.receipt_copies,updated_by=EXCLUDED.updated_by,updated_at=now()`, c.Tenant, strings.TrimSpace(x.BusinessName), strings.TrimSpace(x.LogoURL), strings.TrimSpace(x.Address), strings.TrimSpace(x.Phone), strings.TrimSpace(x.Email), strings.TrimSpace(x.CurrencyCode), strings.TrimSpace(x.CurrencySymbol), x.DefaultTaxRate, strings.TrimSpace(x.InvoicePrefix), strings.TrimSpace(x.ReceiptHeader), strings.TrimSpace(x.ReceiptFooter), x.ReceiptSize, x.ShowTax, x.ShowDiscount, x.LowStockEnabled, x.ExpiryAlertDays, x.DefaultPaymentMethod,strings.TrimSpace(x.BusinessRegistrationNo),strings.TrimSpace(x.TaxRegistrationNo),x.InvoiceNumberDigits,strings.TrimSpace(x.ReceiptTitle),x.ShowBusinessLogo,x.ShowBusinessAddress,x.ShowBusinessContact,x.ReceiptCopies, c.Sub)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		auditUserAction(r, db, "SYSTEM_SETTINGS_UPDATED", c.Tenant, x)
		w.WriteHeader(204)
	}
}
