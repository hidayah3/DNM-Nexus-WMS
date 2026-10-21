package internal

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func movementWorkbook(rows []StockMovement) ([]byte, error) {
	values := make([][]string, 0, len(rows))
	for _, m := range rows {
		values = append(values, []string{m.CreatedAt.Format(time.RFC3339), m.SKU, m.ProductName, m.BatchNumber, m.ExpiryDate, m.Type,
			fmt.Sprint(m.Quantity), fmt.Sprint(m.StockAfter), m.Reason, m.ActorID, m.ActorName})
	}
	return xlsxWorkbook("Stock movements", []string{"Date and time", "SKU", "Product", "Batch number", "Expiry date", "Movement", "Quantity changed", "Stock after", "Reason", "Account ID", "Account name"}, values)
}

func inventoryWorkbook(lots []InventoryLot) ([]byte, error) {
	values := make([][]string, 0, len(lots))
	for _, lot := range lots {
		expiry := ""
		if lot.ExpiryDate != nil {
			expiry = lot.ExpiryDate.Format("2006-01-02")
		}
		values = append(values, []string{lot.ProductName, lot.SKU, lot.BatchNumber, expiry, fmt.Sprint(lot.Quantity), lot.CreatedBy, lot.CreatedName, lot.CreatedAt.Format(time.RFC3339)})
	}
	return xlsxWorkbook("Inventory batches", []string{"Product", "SKU", "Batch number", "Expiry date", "Quantity", "Input account ID", "Input account name", "Date recorded"}, values)
}

func xlsxWorkbook(sheetName string, headers []string, rows [][]string) ([]byte, error) {
	if len(sheetName) > 31 {
		sheetName = sheetName[:31]
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	files := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="` + xmlEscape(sheetName) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
	}
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	writeRow := func(n int, values []string) {
		sheet.WriteString(`<row r="` + strconv.Itoa(n) + `">`)
		for i, value := range values {
			col := excelColumn(i + 1)
			sheet.WriteString(`<c r="` + col + strconv.Itoa(n) + `" t="inlineStr"><is><t xml:space="preserve">` + xmlEscape(value) + `</t></is></c>`)
		}
		sheet.WriteString(`</row>`)
	}
	writeRow(1, headers)
	for i, row := range rows {
		writeRow(i+2, row)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	files["xl/worksheets/sheet1.xml"] = sheet.String()
	for name, content := range files {
		f, err := z.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = f.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func excelColumn(n int) string {
	var column string
	for n > 0 {
		n--
		column = string(rune('A'+n%26)) + column
		n /= 26
	}
	return column
}

func xmlEscape(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}
