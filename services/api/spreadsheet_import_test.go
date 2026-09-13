package main

import("encoding/base64";"strings";"testing")
func TestSpreadsheetRecordsCSV(t *testing.T){rows,e:=spreadsheetRecords("products.csv","name,sku\nTea,T-1","");if e!=nil||len(rows)!=2||rows[1][1]!="T-1"{t.Fatalf("unexpected CSV parse: %#v %v",rows,e)}}
func TestSpreadsheetRecordsXLSX(t *testing.T){book,e:=createXLSX([]string{"name","sku"},[][]string{{"Tea","T-1"}});if e!=nil{t.Fatal(e)};rows,e:=spreadsheetRecords("products.xlsx","",base64.StdEncoding.EncodeToString(book));if e!=nil||len(rows)!=2||rows[0][0]!="name"||rows[1][1]!="T-1"{t.Fatalf("unexpected XLSX parse: %#v %v",rows,e)}}
func TestMappedHeaders(t *testing.T){m:=mappedHeaders([]string{"Item Name","Stock Code"},map[string]string{"name":"Item Name","sku":"Stock Code"});if m["name"]!=0||m["sku"]!=1{t.Fatalf("mapping failed: %#v",m)}}
func TestPDFExport(t *testing.T){doc,e:=createPDF("sales",[]string{"Invoice","Total"},[][]string{{"INV-1","100.00"}});if e!=nil||!strings.HasPrefix(string(doc),"%PDF-1.4")||!strings.Contains(string(doc),"INV-1"){t.Fatal("invalid PDF")}}
