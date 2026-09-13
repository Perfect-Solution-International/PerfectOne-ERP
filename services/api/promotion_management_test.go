package main

import "testing"

func TestPromotionDiscount(t *testing.T) {
	tests:=[]struct{kind string;value,price,want float64}{
		{"percentage",10,1000,100},{"fixed",125,1000,125},{"special_price",750,1000,250},
		{"fixed",1200,1000,1000},{"special_price",1100,1000,0},{"unknown",10,1000,0},
	}
	for _,tt:=range tests { if got:=promotionDiscount(tt.kind,tt.value,tt.price);got!=tt.want{t.Errorf("%s: got %.2f want %.2f",tt.kind,got,tt.want)} }
}

func TestPromotionValidation(t *testing.T) {
	valid:=promotionInput{Name:"Offer",Kind:"percentage",Value:10,ProductID:"p",StartDate:"2026-09-01",EndDate:"2026-09-30",IsActive:true}
	if got:=validPromotion(valid);got!=""{t.Fatalf("valid promotion rejected: %s",got)}
	invalid:=valid;invalid.CategoryID="c";if validPromotion(invalid)==""{t.Fatal("promotion with two targets accepted")}
	invalid=valid;invalid.Value=101;if validPromotion(invalid)==""{t.Fatal("percentage above 100 accepted")}
	invalid=valid;invalid.EndDate="2026-08-31";if validPromotion(invalid)==""{t.Fatal("reverse date range accepted")}
}
