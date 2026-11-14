package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func originalRow(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(etlPath)
	if err != nil {
		t.Fatal(err)
	}
	var b map[string]json.RawMessage
	if err = json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	var rec map[string]json.RawMessage
	if err = json.Unmarshal(b[name], &rec); err != nil {
		t.Fatal(err)
	}
	return rec["stellar_etl_operation_output"]
}

func TestPrepareOriginalRowsPreservesVerdictsAndClaims(t *testing.T) {
	lcm, _ := realEvidence(t)
	for _, tc := range []struct{ name, hash, status string }{
		{"control", controlHash, "PASS"}, {"target", targetHash, "FAIL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := originalRow(t, tc.name)
			raw, err := prepareBundle(lcm, row, tc.hash, 0, mainnetPassphrase)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := decodeETL("prepared", raw)
			if err != nil {
				t.Fatal(err)
			}
			report := verify(lcm, bundle, tc.hash, mainnetPassphrase)
			if report.Status != tc.status {
				t.Fatalf("%+v", report)
			}
			var b struct {
				Target struct {
					Original json.RawMessage `json:"stellar_etl_operation_output"`
				}
			}
			if err = json.Unmarshal(raw, &b); err != nil {
				t.Fatal(err)
			}
			var want, got bytes.Buffer
			if err = json.Compact(&want, row); err != nil {
				t.Fatal(err)
			}
			if err = json.Compact(&got, b.Target.Original); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want.Bytes(), got.Bytes()) {
				t.Fatal("original ETL object changed")
			}
		})
	}
}

func TestPrepareDoesNotRepairDisagreeingMovement(t *testing.T) {
	lcm, _ := realEvidence(t)
	row := bytes.ReplaceAll(originalRow(t, "control"), []byte("15.1300000"), []byte("15.1300001"))
	raw, err := prepareBundle(lcm, row, controlHash, 0, mainnetPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := decodeETL("prepared", raw)
	if err != nil {
		t.Fatal(err)
	}
	report := verify(lcm, bundle, controlHash, mainnetPassphrase)
	if report.Status != "FAIL" || len(report.ExportMovements) != 1 || report.ExportMovements[0].Amount != "15.1300001" {
		t.Fatalf("claim was repaired or lost: %+v", report)
	}
}

func TestPrepareRefusesWrongIdentityAndIncompleteClaims(t *testing.T) {
	lcm, _ := realEvidence(t)
	row := originalRow(t, "control")
	for name, change := range map[string]func(map[string]any){
		"row from another transaction": func(m map[string]any) { m["transaction_id"] = json.Number("277275688402329600") },
		"wrong operation":              func(m map[string]any) { m["id"] = json.Number("277275688402333698") },
		"rounded ID":                   func(m map[string]any) { m["id"] = json.Number("277275688402333700") },
		"wrong ledger":                 func(m map[string]any) { m["ledger_sequence"] = 1 },
		"wrong operation type":         func(m map[string]any) { m["type"] = 1 },
		"missing type":                 func(m map[string]any) { delete(m, "type") },
		"missing changes":              func(m map[string]any) { delete(m["details"].(map[string]any), "asset_balance_changes") },
		"null changes":                 func(m map[string]any) { m["details"].(map[string]any)["asset_balance_changes"] = nil },
	} {
		t.Run(name, func(t *testing.T) {
			dec := json.NewDecoder(bytes.NewReader(row))
			dec.UseNumber()
			var m map[string]any
			if err := dec.Decode(&m); err != nil {
				t.Fatal(err)
			}
			change(m)
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = prepareBundle(lcm, raw, controlHash, 0, mainnetPassphrase); err == nil {
				t.Fatal("accepted unsupported/mismatched row")
			}
		})
	}
	for _, raw := range [][]byte{[]byte(`{"ledger_sequence":1,"ledger_sequence":2}`), []byte(`null`), []byte(`[]`), append(append([]byte{}, row...), row...)} {
		if _, err := prepareBundle(lcm, raw, controlHash, 0, mainnetPassphrase); err == nil {
			t.Fatal("accepted malformed or ambiguous row")
		}
	}
	for _, tc := range []struct {
		hash, pass string
		op         int
	}{
		{controlHash, mainnetPassphrase, -1}, {controlHash, mainnetPassphrase, 1},
		{controlHash, "wrong network", 0}, {"not a hash", mainnetPassphrase, 0},
	} {
		if _, err := prepareBundle(lcm, row, tc.hash, tc.op, tc.pass); err == nil {
			t.Fatal("accepted invalid selection")
		}
	}
}

func TestLedgerStorageFormatsPreserveEvidence(t *testing.T) {
	for _, tc := range []struct{ format, path string }{
		{"base64", ledgerPath},
		{"xdr", "docs/evidence/aws-public-ledger-64558277.metadata.xdr"},
		{"rpc", "docs/evidence/onfinality-getledgers-64558277.json"},
		{"galexie", "docs/evidence/aws-public-ledger-64558277.xdr.zst"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			raw, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			lcm, err := readLedgerInput(tc.path, raw, tc.format, 64558277, mainnetPassphrase)
			if err != nil {
				t.Fatal(err)
			}
			_, bundle := realEvidence(t)
			if report := verify(lcm, bundle, controlHash, mainnetPassphrase); report.Status != "PASS" {
				t.Fatalf("%+v", report)
			}
			if _, err = readLedgerInput(tc.path, raw, tc.format, 64558278, mainnetPassphrase); err == nil {
				t.Fatal("wrong ledger accepted")
			}
		})
	}
	for _, format := range []string{"rpc", "galexie", "xdr", "unsupported"} {
		if _, err := readLedgerInput("bad", []byte("broken"), format, 0, mainnetPassphrase); err == nil {
			t.Fatal("bad storage accepted")
		}
	}
	for _, raw := range []string{
		`{"error":{"code":-32600,"message":"outside retention"}}`,
		`{"result":{"ledgers":[]}}`,
		`{"result":{"ledgers":[]},"result":{"ledgers":[]}}`,
	} {
		if _, err := readLedgerInput("bad", []byte(raw), "rpc", 64558277, mainnetPassphrase); err == nil {
			t.Fatal("bad RPC response accepted")
		}
	}
	raw, err := os.ReadFile("docs/evidence/onfinality-getledgers-64558277.json")
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err = json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	result := response["result"].(map[string]any)
	ledgers := result["ledgers"].([]any)
	result["ledgers"] = append(ledgers, ledgers[0])
	duplicate, _ := json.Marshal(response)
	if _, err = readLedgerInput("duplicate", duplicate, "rpc", 64558277, mainnetPassphrase); err == nil {
		t.Fatal("ambiguous RPC ledger accepted")
	}
	result["ledgers"] = ledgers
	ledgers[0].(map[string]any)["hash"] = strings.Repeat("0", 64)
	wrongHash, _ := json.Marshal(response)
	if _, err = readLedgerInput("wrong hash", wrongHash, "rpc", 64558277, mainnetPassphrase); err == nil {
		t.Fatal("wrong RPC hash accepted")
	}
}

func TestPrepareCLIAndHelp(t *testing.T) {
	temp := t.TempDir()
	row := filepath.Join(temp, "operation.json")
	if err := os.WriteFile(row, originalRow(t, "control"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"prepare", "--ledger-meta", ledgerPath, "--operation-output", row, "--transaction-hash", controlHash, "--operation-index", "0", "--network-passphrase", mainnetPassphrase}
	var out bytes.Buffer
	if code := run(args, &out); code != 0 {
		t.Fatalf("exit=%d %s", code, out.String())
	}
	bundle := filepath.Join(temp, "bundle.json")
	if err := os.WriteFile(bundle, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"--ledger-meta", ledgerPath, "--etl-output", bundle, "--transaction-hash", controlHash, "--network-passphrase", mainnetPassphrase}, &out); code != 0 {
		t.Fatalf("exit=%d %s", code, out.String())
	}
	for _, args := range [][]string{{"--help"}, {"prepare", "--help"}} {
		out.Reset()
		if code := run(args, &out); code != 0 || !strings.Contains(out.String(), "Usage:") {
			t.Fatalf("exit=%d %s", code, out.String())
		}
	}
	out.Reset()
	if code := run([]string{"prepare"}, &out); code != 3 || !strings.Contains(out.String(), "UNKNOWN") {
		t.Fatalf("exit=%d %s", code, out.String())
	}
	out.Reset()
	if code := run([]string{"unrecognized"}, &out); code != 3 {
		t.Fatalf("exit=%d %s", code, out.String())
	}
	out.Reset()
	if code := run([]string{"--ledger-meta", ledgerPath, "--etl-output", row, "--transaction-hash", controlHash, "--network-passphrase", mainnetPassphrase}, &out); code != 3 || !strings.Contains(out.String(), "prepare --help") {
		t.Fatalf("exit=%d %s", code, out.String())
	}
}
