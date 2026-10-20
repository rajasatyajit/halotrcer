// Package ingest decodes the two tools' JSON outputs into the subset of
// documented fields Halotrcer uses. Unknown fields are ignored (both tools
// only ever add fields), and each document's schema is gated through
// compat before any other field is trusted.
//
// Field names follow the tools' frozen contracts:
//   - Tokvera report.json: tokvera cmd/tokvera/testdata/golden/surface/json-scan-report.txt
//     (schema tokvera.scan-report/v1.x, docs/report-anatomy.md).
//   - Sievant scan --json: sievant pkg/scan/report.go and pkg/scan/scan.go (report_version 1).
//   - Sievant report --format json: sievant pkg/report/report.go (report_version 1).
//   - Both tools' doctor --json: {checks[{name,status,detail,fix}],warnings,failures}.
package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/rajasatyajit/halotrcer/internal/compat"
)

// MaxDocument bounds every document read from a tool (the largest real
// report in the fixtures is ~16 KB).
const MaxDocument = 32 << 20

func readBounded(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxDocument+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxDocument {
		return nil, fmt.Errorf("document larger than %d bytes", MaxDocument)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, errors.New("empty document")
	}
	return b, nil
}

// DecodeTokvera reads a Tokvera report.json.
func DecodeTokvera(r io.Reader, m compat.Matrix) (*TokveraReport, error) {
	b, err := readBounded(r)
	if err != nil {
		return nil, fmt.Errorf("tokvera report: %w", err)
	}
	var head struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, fmt.Errorf("tokvera report: %w", err)
	}
	if err := m.CheckTokveraSchema(head.SchemaVersion); err != nil {
		return nil, err
	}
	var rep TokveraReport
	if err := json.Unmarshal(b, &rep); err != nil {
		return nil, fmt.Errorf("tokvera report: %w", err)
	}
	return &rep, nil
}

// DecodeSievantScan reads `sievant scan --json` output.
func DecodeSievantScan(r io.Reader, m compat.Matrix) (*SievantScan, error) {
	b, err := readBounded(r)
	if err != nil {
		return nil, fmt.Errorf("sievant scan: %w", err)
	}
	var head struct {
		ReportVersion int `json:"report_version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, fmt.Errorf("sievant scan: %w", err)
	}
	if err := m.CheckSievantReport(head.ReportVersion); err != nil {
		return nil, err
	}
	var rep SievantScan
	if err := json.Unmarshal(b, &rep); err != nil {
		return nil, fmt.Errorf("sievant scan: %w", err)
	}
	if rep.Findings == nil {
		rep.Findings = []SievantFinding{}
	}
	return &rep, nil
}

// DecodeSievantGateway reads `sievant report --format json` output.
func DecodeSievantGateway(r io.Reader, m compat.Matrix) (*SievantGateway, error) {
	b, err := readBounded(r)
	if err != nil {
		return nil, fmt.Errorf("sievant report: %w", err)
	}
	var head struct {
		ReportVersion int `json:"report_version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, fmt.Errorf("sievant report: %w", err)
	}
	if err := m.CheckSievantReport(head.ReportVersion); err != nil {
		return nil, err
	}
	var rep SievantGateway
	if err := json.Unmarshal(b, &rep); err != nil {
		return nil, fmt.Errorf("sievant report: %w", err)
	}
	return &rep, nil
}

// DecodeDoctor reads either tool's `doctor --json` output.
func DecodeDoctor(r io.Reader) (*Doctor, error) {
	b, err := readBounded(r)
	if err != nil {
		return nil, fmt.Errorf("doctor: %w", err)
	}
	var d Doctor
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("doctor: %w", err)
	}
	if d.Checks == nil {
		return nil, errors.New("doctor: no checks in output")
	}
	return &d, nil
}
