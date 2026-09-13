package main

import (
	"net/http/httptest"
	"testing"
)

func TestRequestPermissionForHighRiskRoutes(t *testing.T) {
	tests := []struct{ method, path, expected string }{
		{"POST", "/purchase-orders/abc/approve", "purchases.approve"},
		{"POST", "/purchases/abc/reverse", "purchases.cancel"},
		{"POST", "/inventory/transfers/abc/dispatch-lines", "inventory.approve"},
		{"POST", "/inventory/adjustment-requests/abc/finalize", "inventory.approve"},
		{"POST", "/sales/abc/cancel", "sales.cancel"},
		{"POST", "/sales/abc/returns", "sales_returns.add"},
		{"DELETE", "/users/abc", "users.delete"},
		{"PUT", "/roles/abc/permissions", "roles.edit"},
		{"GET", "/reports/profit-loss?format=csv", "reports.export"},
		{"GET", "/reports/profit-loss?format=xlsx", "reports.export"},
		{"GET", "/reports/profit-loss", "reports.view"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, nil)
		if actual := requestPermission(request); actual != test.expected { t.Errorf("%s %s: got %q, want %q", test.method, test.path, actual, test.expected) }
	}
}

func TestRoleDefaultsDoNotExposeFinancialReportsToCashier(t *testing.T) {
	if roleHas("cashier", "financial_reports") || roleHas("cashier", "accounting") || roleHas("cashier", "purchases") { t.Fatal("cashier default role exposes restricted finance or purchase access") }
	if !roleHas("cashier", "pos") || !roleHas("cashier", "own_sales") { t.Fatal("cashier default role is missing required POS permissions") }
}
