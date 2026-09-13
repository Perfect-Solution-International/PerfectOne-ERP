package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

type xlsxSharedStrings struct { Items []struct { Text string `xml:"t"`; Runs []struct{ Text string `xml:"t"` } `xml:"r"` } `xml:"si"` }
type xlsxWorksheet struct { Rows []struct { Cells []struct { Ref string `xml:"r,attr"`; Type string `xml:"t,attr"`; Value string `xml:"v"`; Inline struct { Text string `xml:"t"`; Runs []struct{ Text string `xml:"t"` } `xml:"r"` } `xml:"is"` } `xml:"c"` } `xml:"sheetData>row"` }

func spreadsheetRecords(fileName, csvText, encoded string) ([][]string, error) {
	ext:=strings.ToLower(filepath.Ext(fileName)); if ext==".xlsx" { raw,e:=base64.StdEncoding.DecodeString(encoded);if e!=nil{return nil,errors.New("invalid Excel upload")};return readXLSX(raw) }
	if csvText==""&&encoded!=""{raw,e:=base64.StdEncoding.DecodeString(encoded);if e!=nil{return nil,errors.New("invalid CSV upload")};csvText=string(bytes.TrimPrefix(raw,[]byte{0xef,0xbb,0xbf}))}
	r:=csv.NewReader(strings.NewReader(csvText));r.TrimLeadingSpace=true;r.FieldsPerRecord=-1;records,e:=r.ReadAll();if e!=nil{return nil,fmt.Errorf("invalid CSV: %w",e)};return records,nil
}
func readXLSX(raw []byte)([][]string,error){
	z,e:=zip.NewReader(bytes.NewReader(raw),int64(len(raw)));if e!=nil{return nil,errors.New("invalid XLSX file")};files:=map[string]*zip.File{};for _,f:=range z.File{files[f.Name]=f}
	shared:=[]string{};if f:=files["xl/sharedStrings.xml"];f!=nil{b,_:=readZipFile(f);var x xlsxSharedStrings;if xml.Unmarshal(b,&x)==nil{for _,item:=range x.Items{v:=item.Text;for _,run:=range item.Runs{v+=run.Text};shared=append(shared,v)}}}
	sheet:=files["xl/worksheets/sheet1.xml"];if sheet==nil{return nil,errors.New("XLSX first worksheet is missing")};b,e:=readZipFile(sheet);if e!=nil{return nil,e};var ws xlsxWorksheet;if e=xml.Unmarshal(b,&ws);e!=nil{return nil,errors.New("cannot read XLSX worksheet")}
	rows:=[][]string{};for _,row:=range ws.Rows{max:=0;for _,cell:=range row.Cells{if n:=xlsxColumn(cell.Ref);n>max{max=n}};values:=make([]string,max+1);for _,cell:=range row.Cells{value:=cell.Value;if cell.Type=="s"{i,_:=strconv.Atoi(value);if i>=0&&i<len(shared){value=shared[i]}}else if cell.Type=="inlineStr"{value=cell.Inline.Text;for _,run:=range cell.Inline.Runs{value+=run.Text}};values[xlsxColumn(cell.Ref)]=strings.TrimSpace(value)};rows=append(rows,values)};if len(rows)<2{return nil,errors.New("spreadsheet must contain a header and at least one row")};return rows,nil
}
func readZipFile(f *zip.File)([]byte,error){r,e:=f.Open();if e!=nil{return nil,e};defer r.Close();return io.ReadAll(io.LimitReader(r,20<<20))}
func xlsxColumn(ref string)int{n:=0;for _,r:=range ref{if r<'A'||r>'Z'{break};n=n*26+int(r-'A'+1)};if n==0{return 0};return n-1}

func mappedHeaders(header []string,mapping map[string]string)map[string]int{source:=map[string]int{};for i,h:=range header{source[strings.ToLower(strings.TrimSpace(h))]=i};out:=map[string]int{};for canonical,selected:=range mapping{if i,ok:=source[strings.ToLower(strings.TrimSpace(selected))];ok{out[strings.ToLower(canonical)]=i}};for name,i:=range source{if _,ok:=out[name];!ok{out[name]=i}};return out}
