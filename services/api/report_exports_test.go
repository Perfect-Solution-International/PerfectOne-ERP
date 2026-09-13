package main

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestCreateXLSXProducesReadableWorkbook(t *testing.T) {
	content, err := createXLSX([]string{"Product", "Amount"}, [][]string{{"Tea & Coffee <500g>", "1250.00"}})
	if err != nil { t.Fatalf("createXLSX: %v", err) }
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil { t.Fatalf("workbook is not a ZIP package: %v", err) }
	wanted := map[string]bool{"[Content_Types].xml": false, "xl/workbook.xml": false, "xl/styles.xml": false, "xl/worksheets/sheet1.xml": false}
	var sheet string
	for _, file := range reader.File {
		if _, ok := wanted[file.Name]; ok { wanted[file.Name] = true }
		if file.Name == "xl/worksheets/sheet1.xml" { stream, openErr := file.Open(); if openErr != nil { t.Fatal(openErr) }; body, readErr := io.ReadAll(stream); _ = stream.Close(); if readErr != nil { t.Fatal(readErr) }; sheet = string(body) }
	}
	for name, found := range wanted { if !found { t.Errorf("missing XLSX part %s", name) } }
	if !strings.Contains(sheet, "Tea &amp; Coffee &lt;500g&gt;") { t.Errorf("cell text was not XML escaped: %s", sheet) }
	if !strings.Contains(sheet, `<autoFilter ref="A1:B2"/>`) { t.Error("worksheet filter range missing") }
}

func TestExcelColumn(t *testing.T) {
	cases := map[int]string{0: "A", 25: "Z", 26: "AA", 51: "AZ", 52: "BA"}
	for input, expected := range cases { if actual := excelColumn(input); actual != expected { t.Errorf("excelColumn(%d)=%s, want %s", input, actual, expected) } }
}
