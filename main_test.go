package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

const (
	mainnetPassphrase = "Public Global Stellar Network ; September 2015"
	targetHash        = "575f16436ec1d6eabfc8f66e2dec553e61b53ed85914b50736c842a0d567266b"
	controlHash       = "afbf759fa9317cc7a8e28812c8b5b0245e9d31e55e9e9d8cb6a72e5eb832d57d"
	ledgerPath        = "docs/evidence/ledger-64558277-metadata.xdr.base64"
	etlPath           = "docs/evidence/stellar-etl-62e06b1-ledger-64558277-reproduction.json"
)

func realEvidence(t *testing.T) (xdr.LedgerCloseMeta, ETLBundle) {
	t.Helper()
	ledgerBytes, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	lcm, err := readLedger(ledgerPath, ledgerBytes, mainnetPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	etlBytes, err := os.ReadFile(etlPath)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := decodeETL(etlPath, etlBytes)
	if err != nil {
		t.Fatal(err)
	}
	return lcm, bundle
}

func transfer(amount, from, to string) ExportMovement {
	return ExportMovement{Type: "transfer", AssetType: "credit_alphanum4", AssetCode: "USDC", AssetIssuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN", Amount: amount, From: from, To: to}
}

func TestRealFailedTargetFailsForCanonicalExecutionReason(t *testing.T) {
	lcm, bundle := realEvidence(t)
	report := verify(lcm, bundle, targetHash, mainnetPassphrase)
	if report.Status != "FAIL" {
		t.Fatalf("got %s: %s", report.Status, report.Reason)
	}
	if report.TransactionSuccess == nil || *report.TransactionSuccess {
		t.Fatalf("canonical result not failed: %+v", report.TransactionSuccess)
	}
	if len(report.CanonicalMovements) != 0 {
		t.Fatalf("failed tx has canonical committed transfers: %+v", report.CanonicalMovements)
	}
	if len(report.ExportMovements) != 9 {
		t.Fatalf("expected nine actual ETL transformer records, got %d", len(report.ExportMovements))
	}
	if report.FeeChargedStroops != "174334" {
		t.Fatalf("fee missing or wrong: %s", report.FeeChargedStroops)
	}
}

func TestRealSuccessfulControlPassesFromCommittedOperationEvents(t *testing.T) {
	lcm, bundle := realEvidence(t)
	report := verify(lcm, bundle, controlHash, mainnetPassphrase)
	if report.Status != "PASS" {
		t.Fatalf("got %s: %s", report.Status, report.Reason)
	}
	if report.TransactionSuccess == nil || !*report.TransactionSuccess {
		t.Fatal("canonical control result not successful")
	}
	if len(report.CanonicalMovements) != 1 || len(report.ExportMovements) != 1 {
		t.Fatalf("unexpected movement counts: canonical=%d export=%d", len(report.CanonicalMovements), len(report.ExportMovements))
	}
	want := transfer("15.1300000", "GCNOWLX3ICSI5JVM4M443Y2WGOTJROVQLRWOEXGOJ6GM6J4CLNJ4IEQA", "CCDCTXGN7FPCGSPV4PEVNZODDNHUMDQ3SAKFHYW6TCCDK3JXSX4UVS3Y")
	if report.CanonicalMovements[0] != want || report.ExportMovements[0] != want {
		t.Fatalf("control transfer mismatch: canonical=%+v export=%+v", report.CanonicalMovements[0], report.ExportMovements[0])
	}
}

func TestMissingCanonicalEvidenceIsUnknownWithStrictExit(t *testing.T) {
	var out bytes.Buffer
	code := run(nil, &out)
	var report EvidenceReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if code != 3 || report.Status != "UNKNOWN" {
		t.Fatalf("exit=%d report=%+v", code, report)
	}
}

func TestMissingETLRecordIsUnknown(t *testing.T) {
	lcm, bundle := realEvidence(t)
	bundle.Target = nil
	report := verify(lcm, bundle, targetHash, mainnetPassphrase)
	if report.Status != "UNKNOWN" || report.Reason != "matching ETL operation record is missing" {
		t.Fatalf("got %+v", report)
	}
}

func TestAmbiguousETLTransactionIdentityIsUnknown(t *testing.T) {
	_, bundle := realEvidence(t)
	bundle.Control = bundle.Target
	if _, err := selectETLRecord(bundle, targetHash); err == nil {
		t.Fatal("duplicate transaction identity was accepted")
	}
}

func TestDuplicateExportMovementIsDetected(t *testing.T) {
	one := transfer("15.1300000", "from", "to")
	report := compareTransfers(true, []ExportMovement{one}, []ExportMovement{one, one})
	if report.Status != "FAIL" {
		t.Fatalf("got %+v", report)
	}
}

func TestChangedAmountAndRecipientAreDetected(t *testing.T) {
	canonical := transfer("15.1300000", "from", "to")
	for name, exported := range map[string]ExportMovement{
		"amount":    transfer("15.1300001", "from", "to"),
		"recipient": transfer("15.1300000", "from", "other"),
		"sender":    transfer("15.1300000", "other", "to"),
		"asset": func() ExportMovement {
			m := transfer("15.1300000", "from", "to")
			m.AssetIssuer = "GDIFFERENTISSUER"
			return m
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			report := compareTransfers(true, []ExportMovement{canonical}, []ExportMovement{exported})
			if report.Status != "FAIL" {
				t.Fatalf("got %+v", report)
			}
		})
	}
}

func TestUnsupportedMovementIsUnknown(t *testing.T) {
	bad := transfer("1.0", "from", "to")
	bad.AssetType = "unknown"
	report := compareTransfers(true, nil, []ExportMovement{bad})
	if report.Status != "UNKNOWN" {
		t.Fatalf("got %+v", report)
	}
	bad = transfer("1.00000000", "from", "to")
	report = compareTransfers(true, nil, []ExportMovement{bad})
	if report.Status != "UNKNOWN" {
		t.Fatalf("got %+v", report)
	}
}

func TestFeeOnlyFailedTransactionDoesNotCreateAssetMovement(t *testing.T) {
	report := compareTransfers(false, []ExportMovement{}, []ExportMovement{})
	if report.Status != "PASS" {
		t.Fatalf("fee-only failure should have no asset movement discrepancy: %+v", report)
	}
}

func TestWrongNetworkAndMismatchedLedgerAreUnknown(t *testing.T) {
	lcm, bundle := realEvidence(t)
	report := verify(lcm, bundle, targetHash, "Test SDF Network ; September 2015")
	if report.Status != "UNKNOWN" {
		t.Fatalf("wrong network passphrase got %+v", report)
	}
	bundle.LedgerSequence++
	report = verify(lcm, bundle, targetHash, mainnetPassphrase)
	if report.Status != "UNKNOWN" {
		t.Fatalf("wrong ledger got %+v", report)
	}
}

func TestOperationIdentityMismatchIsUnknown(t *testing.T) {
	lcm, bundle := realEvidence(t)
	bundle.Control.OperationIndex = 1
	report := verify(lcm, bundle, controlHash, mainnetPassphrase)
	if report.Status != "UNKNOWN" {
		t.Fatalf("got %+v", report)
	}
}

func TestExactStroopParsing(t *testing.T) {
	cases := map[string]int64{"0.0000001": 1, "15.1300000": 151300000, "922337203685.4775807": 9223372036854775807}
	for input, want := range cases {
		got, err := parseExportStroops(input)
		if err != nil || got != want {
			t.Fatalf("parseExportStroops(%q)=(%d,%v), want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"0", "0.0000000", "1e-7", "1.00000000", "922337203685.4775808", "-1.0", "1."} {
		if _, err := parseExportStroops(input); err == nil {
			t.Fatalf("accepted invalid amount %q", input)
		}
	}
}

func TestMalformedCanonicalAndETLEvidenceAreUnknown(t *testing.T) {
	if _, err := readLedger("broken", []byte("not-base64"), mainnetPassphrase); err == nil {
		t.Fatal("malformed XDR accepted")
	}
	if _, err := decodeETL("broken", []byte("{")); err == nil {
		t.Fatal("malformed ETL JSON accepted")
	}
}

func TestDuplicateJSONKeysAreRejectedAsAmbiguous(t *testing.T) {
	inputs := []string{
		`{"ledger_sequence":64558277,"ledger_sequence":64558278}`,
		`{"ledger_sequence":64558277,"target":{"transaction_hash":"a","transaction_hash":"b"}}`,
	}
	for _, input := range inputs {
		if _, err := decodeETL("duplicate.json", []byte(input)); err == nil {
			t.Fatalf("duplicate JSON keys were accepted: %s", input)
		}
	}
}

func TestETLIdentityMismatchIsUnknown(t *testing.T) {
	lcm, bundle := realEvidence(t)
	bundle.Target.TransactionHash = controlHash
	report := verify(lcm, bundle, targetHash, mainnetPassphrase)
	if report.Status != "UNKNOWN" {
		t.Fatalf("got %+v", report)
	}
}

func TestCommittedResultMutationCannotPass(t *testing.T) {
	lcm, bundle := realEvidence(t)
	idx := int(bundle.Control.TransactionIndex - 1)
	if idx < 0 || idx >= len(lcm.V2.TxProcessing) {
		t.Fatalf("control result index out of range: %d", idx)
	}
	// Alter a result field while leaving the committed ledger header intact,
	// then make the exported wrapper internally agree with the altered value.
	lcm.V2.TxProcessing[idx].Result.Result.FeeCharged++
	bundle.Control.FeeChargedStroops = "13164"
	report := verify(lcm, bundle, controlHash, mainnetPassphrase)
	if report.Status != "UNKNOWN" || !strings.Contains(report.Reason, "transaction result set hash") {
		t.Fatalf("uncommitted result mutation was not rejected by the header commitment: %+v", report)
	}
}

func TestMutatedTransactionSetCommitmentIsUnknown(t *testing.T) {
	lcm, bundle := realEvidence(t)
	lcm.V2.TxSet.V1TxSet.PreviousLedgerHash[0] ^= 1
	report := verify(lcm, bundle, controlHash, mainnetPassphrase)
	if report.Status != "UNKNOWN" || !strings.Contains(report.Reason, "transaction set hash") {
		t.Fatalf("mutated transaction set was not rejected as incomplete canonical evidence: %+v", report)
	}
}

func TestMalformedCanonicalTransferEventIsUnknown(t *testing.T) {
	lcm, bundle := realEvidence(t)
	idx := int(bundle.Control.TransactionIndex - 1)
	meta := lcm.V2.TxProcessing[idx].TxApplyProcessing
	events := meta.V4.Operations[bundle.Control.OperationIndex].Events
	if len(events) != 1 || events[0].Body.V0 == nil || len(events[0].Body.V0.Topics) != 4 {
		t.Fatalf("unexpected real control event shape: %+v", events)
	}
	events[0].Body.V0.Topics = events[0].Body.V0.Topics[:3]
	meta.V4.Operations[bundle.Control.OperationIndex].Events = events
	lcm.V2.TxProcessing[idx].TxApplyProcessing = meta
	report := verify(lcm, bundle, controlHash, mainnetPassphrase)
	if report.Status != "UNKNOWN" || !strings.Contains(report.Reason, "unsupported or malformed contract transfer event") {
		t.Fatalf("malformed canonical transfer event was not rejected: %+v", report)
	}
}

func TestSuccessfulPartialOperationMetadataIsRejected(t *testing.T) {
	meta := xdr.TransactionMeta{V: 4, V4: &xdr.TransactionMetaV4{Operations: []xdr.OperationMetaV2{{}}}}
	if _, err := committedTransfers(meta, 0, 2, mainnetPassphrase, true); err == nil {
		t.Fatal("partial successful-transaction operation metadata was accepted")
	}
}

func TestMissingExpectedMovementFails(t *testing.T) {
	canonical := transfer("15.1300000", "from", "to")
	report := compareTransfers(true, []ExportMovement{canonical}, []ExportMovement{})
	if report.Status != "FAIL" {
		t.Fatalf("missing exported movement got %+v", report)
	}
}

func TestMovementInputOrderDoesNotChangeVerdict(t *testing.T) {
	a := transfer("1.0000000", "a", "b")
	b := transfer("2.0000000", "c", "d")
	report := compareTransfers(true, []ExportMovement{a, b}, []ExportMovement{b, a})
	if report.Status != "PASS" {
		t.Fatalf("reordered matching movements got %+v", report)
	}
}

func TestEvidenceFilesAreDistinctAndPresent(t *testing.T) {
	for _, p := range []string{ledgerPath, etlPath} {
		abs, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(abs); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMalformedMovementMissingSourceYieldsUnknown(t *testing.T) {
	badMovement := ExportMovement{
		Type:   "transfer",
		From:   "", // missing from
		To:     "destination_account",
		Amount: "100.0000000",
	}
	report := compareTransfers(true, []ExportMovement{badMovement}, []ExportMovement{})
	if report.Status != "UNKNOWN" {
		t.Fatalf("expected UNKNOWN status for malformed movement, got %+v", report)
	}
}

func TestFormatStroopsToDecimalRoundtrip(t *testing.T) {
	testValues := []int64{1, 10, 5000000, 10000000, 1250000000, 999999999999}
	for _, v := range testValues {
		str, err := FormatStroopsToDecimal(v)
		if err != nil {
			t.Fatalf("unexpected error formatting %d: %v", v, err)
		}
		parsed, err := parseExportStroops(str)
		if err != nil {
			t.Fatalf("unexpected error parsing %s (from %d): %v", str, v, err)
		}
		if parsed != v {
			t.Fatalf("roundtrip mismatch: expected %d, got %d (string %s)", v, parsed, str)
		}
	}
}

func TestFormatStroopsRejectsZeroAndNegative(t *testing.T) {
	if _, err := FormatStroopsToDecimal(0); err == nil {
		t.Fatal("expected error for zero stroops, got nil")
	}
	if _, err := FormatStroopsToDecimal(-500); err == nil {
		t.Fatal("expected error for negative stroops, got nil")
	}
}

func TestDiffMovementsDiscrepancies(t *testing.T) {
	m1 := transfer("10.0000000", "GAAA", "GBBB")
	m2 := transfer("25.0000000", "GBBB", "GCCC")
	m3 := transfer("5.0000000", "GCCC", "GDDD")

	// Canonical has m1, m2; ETL only has m1 -> m2 is missing in ETL
	diff1 := DiffMovements([]ExportMovement{m1, m2}, []ExportMovement{m1})
	if len(diff1.MissingInETL) != 1 || diff1.MissingInETL[0].Amount != "25.0000000" {
		t.Fatalf("expected m2 missing in ETL, got %+v", diff1.MissingInETL)
	}
	if len(diff1.SurplusInETL) != 0 {
		t.Fatalf("expected no surplus in ETL, got %+v", diff1.SurplusInETL)
	}

	// Canonical has m1; ETL has m1 and m3 -> m3 is surplus in ETL
	diff2 := DiffMovements([]ExportMovement{m1}, []ExportMovement{m1, m3})
	if len(diff2.SurplusInETL) != 1 || diff2.SurplusInETL[0].Amount != "5.0000000" {
		t.Fatalf("expected m3 surplus in ETL, got %+v", diff2.SurplusInETL)
	}
	if len(diff2.MissingInETL) != 0 {
		t.Fatalf("expected no missing in ETL, got %+v", diff2.MissingInETL)
	}

	// Identical sets -> no diff
	diff3 := DiffMovements([]ExportMovement{m1, m2}, []ExportMovement{m2, m1})
	if len(diff3.MissingInETL) != 0 || len(diff3.SurplusInETL) != 0 {
		t.Fatalf("expected empty diff for identical movements, got %+v", diff3)
	}
}

func TestFilterMovementsByAssetAndIssuer(t *testing.T) {
	mUSDC := ExportMovement{AssetCode: "USDC", AssetIssuer: "G_USDC", Amount: "100.0000000"}
	mEURC := ExportMovement{AssetCode: "EURC", AssetIssuer: "G_EURC", Amount: "50.0000000"}
	mCustom := ExportMovement{AssetCode: "USDC", AssetIssuer: "G_OTHER", Amount: "25.0000000"}

	list := []ExportMovement{mUSDC, mEURC, mCustom}

	// Filter by code only
	usdcList := FilterMovements(list, "USDC", "")
	if len(usdcList) != 2 {
		t.Fatalf("expected 2 USDC movements, got %d", len(usdcList))
	}

	// Filter by code and issuer
	exactList := FilterMovements(list, "USDC", "G_USDC")
	if len(exactList) != 1 || exactList[0].Amount != "100.0000000" {
		t.Fatalf("expected 1 exact USDC movement, got %+v", exactList)
	}

	// Filter with empty criteria returns all
	allList := FilterMovements(list, "", "")
	if len(allList) != 3 {
		t.Fatalf("expected 3 movements with empty filter, got %d", len(allList))
	}
}

func TestEvidenceReportJSONAndStatusHelpers(t *testing.T) {
	passReport := EvidenceReport{Status: "PASS", Reason: "matched"}
	failReport := EvidenceReport{Status: "FAIL", Reason: "mismatch"}
	unknownReport := unknown("missing data")

	if !passReport.IsPass() || passReport.IsFail() {
		t.Fatalf("expected passReport to be Pass and not Fail")
	}
	if !failReport.IsFail() || failReport.IsPass() {
		t.Fatalf("expected failReport to be Fail and not Pass")
	}
	if unknownReport.IsPass() || unknownReport.IsFail() {
		t.Fatalf("expected unknownReport to neither be Pass nor Fail")
	}

	raw, err := passReport.ToJSON()
	if err != nil {
		t.Fatalf("failed to serialize report to JSON: %v", err)
	}
	var decoded EvidenceReport
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("failed to deserialize report JSON: %v", err)
	}
	if decoded.Status != "PASS" || decoded.Reason != "matched" {
		t.Fatalf("unexpected decoded report: %+v", decoded)
	}
}

func TestCliVerboseFlagWithValidExecution(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"--ledger-meta", ledgerPath,
		"--etl-output", etlPath,
		"--transaction-hash", targetHash,
		"--network-passphrase", mainnetPassphrase,
		"-v",
	}
	code := run(args, &out)
	if code != 2 {
		t.Fatalf("expected exit code 2 (FAIL), got %d", code)
	}
	var rep EvidenceReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("stdout is not valid JSON with -v flag: %v (output: %s)", err, out.String())
	}
	if rep.Status != "FAIL" {
		t.Fatalf("expected status FAIL, got %s", rep.Status)
	}
}

func TestSummarizeMovements(t *testing.T) {
	m1 := transfer("10.0000000", "GA", "GB")
	m2 := transfer("5.5000000", "GB", "GC")
	summary, err := SummarizeMovements([]ExportMovement{m1, m2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.MovementCount != 2 {
		t.Fatalf("expected 2 movements, got %d", summary.MovementCount)
	}
	if summary.TotalStroops != 155_000_000 {
		t.Fatalf("expected 155000000 stroops, got %d", summary.TotalStroops)
	}
	if summary.UniqueAccounts != 3 {
		t.Fatalf("expected 3 unique accounts (GA, GB, GC), got %d", summary.UniqueAccounts)
	}
}

func TestExportMovementsCSV(t *testing.T) {
	m := transfer("10.0000000", "GA", "GB")
	csv := ExportMovementsCSV([]ExportMovement{m})
	expectedHeader := "from,to,amount,asset_type,asset_code,asset_issuer\n"
	if !strings.HasPrefix(csv, expectedHeader) {
		t.Fatalf("expected CSV to start with header, got %q", csv)
	}
	expectedRow := "GA,GB,10.0000000,credit_alphanum4,USDC,GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN\n"
	if !strings.Contains(csv, expectedRow) {
		t.Fatalf("expected CSV to contain row %q, got %q", expectedRow, csv)
	}
}

func TestFilterMovementsByMinAmount(t *testing.T) {
	dust := transfer("0.0000001", "GA", "GB")    // 1 stroop
	normal := transfer("10.0000000", "GA", "GB") // 100,000,000 stroops
	movements := []ExportMovement{dust, normal}

	filtered := FilterMovementsByMinAmount(movements, 1_000_000)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 movement after dust filtering, got %d", len(filtered))
	}
	if filtered[0].Amount != "10.0000000" {
		t.Fatalf("expected 10.0000000, got %s", filtered[0].Amount)
	}

	all := FilterMovementsByMinAmount(movements, 0)
	if len(all) != 2 {
		t.Fatalf("expected 2 movements with zero threshold, got %d", len(all))
	}
}

func TestCliThresholdFlag(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"--ledger-meta", ledgerPath,
		"--etl-output", etlPath,
		"--transaction-hash", targetHash,
		"--network-passphrase", mainnetPassphrase,
		"--threshold", "20000000000000",
	}
	code := run(args, &out)
	if code != 2 {
		t.Fatalf("expected exit code 2 (FAIL), got %d", code)
	}
	var rep EvidenceReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if len(rep.ExportMovements) != 0 {
		t.Fatalf("expected 0 export movements with huge threshold, got %d", len(rep.ExportMovements))
	}
}

func TestMockBundlePairFixture(t *testing.T) {
	fixturePath := filepath.Join("examples", "fixtures", "mock_bundle_pair.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read mock_bundle_pair.json: %v", err)
	}
	var bundle ETLBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("unmarshal mock_bundle_pair: %v", err)
	}
	if bundle.LedgerSequence != 1234567 {
		t.Fatalf("expected ledger_sequence 1234567, got %d", bundle.LedgerSequence)
	}
	if bundle.Target == nil || bundle.Target.OperationType != "payment" {
		t.Fatalf("expected target operation_type payment")
	}
}

func TestValidateAssetIssuer(t *testing.T) {
	// Native asset with no issuer -> ok
	if err := ValidateAssetIssuer("", "native"); err != nil {
		t.Fatalf("native asset should not error with empty issuer: %v", err)
	}
	// Native asset with issuer -> err
	if err := ValidateAssetIssuer("GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5", "native"); err == nil {
		t.Fatalf("native asset with issuer should error")
	}

	// Valid 56-char G... public key -> ok
	validIssuer := "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"
	if err := ValidateAssetIssuer(validIssuer, "credit_alphanum4"); err != nil {
		t.Fatalf("valid issuer should not error: %v", err)
	}

	// Invalid prefix
	if err := ValidateAssetIssuer("CBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5", "credit_alphanum4"); err == nil {
		t.Fatalf("expected error for non-G prefix")
	}

	// Invalid length
	if err := ValidateAssetIssuer("GBBD47", "credit_alphanum4"); err == nil {
		t.Fatalf("expected error for short issuer")
	}
}

func TestFormatMarkdownScorecard(t *testing.T) {
	rep := EvidenceReport{
		Status:         "PASS",
		Reason:         "canonical and ETL records agree",
		LedgerSequence: 1234567,
		CanonicalMovements: []ExportMovement{
			{Amount: "100.0000000", From: "ALICE", To: "BOB"},
		},
		ExportMovements: []ExportMovement{
			{Amount: "100.0000000", From: "ALICE", To: "BOB"},
		},
	}
	md := rep.FormatMarkdownScorecard()
	if !strings.Contains(md, "# Stellar TruthCheck Verification Scorecard") {
		t.Fatalf("scorecard missing header: %s", md)
	}
	if !strings.Contains(md, "- **Status**: PASS") {
		t.Fatalf("scorecard missing status: %s", md)
	}
	if !strings.Contains(md, "- **Canonical Transfers**: 1") {
		t.Fatalf("scorecard missing transfers count: %s", md)
	}

	guidePath := filepath.Join("docs", "CLI_GUIDE.md")
	data, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatalf("read CLI_GUIDE.md: %v", err)
	}
	guideStr := string(data)
	if !strings.Contains(guideStr, "Stellar TruthCheck Offline Verification Guide") {
		t.Fatalf("CLI guide missing title")
	}
}

func TestValidateMemo(t *testing.T) {
	// None
	if err := ValidateMemo("none", ""); err != nil {
		t.Fatalf("valid none memo failed: %v", err)
	}
	if err := ValidateMemo("none", "something"); err == nil {
		t.Fatalf("expected error for non-empty none memo")
	}

	// Text
	if err := ValidateMemo("text", "hello stellar"); err != nil {
		t.Fatalf("valid text memo failed: %v", err)
	}
	if err := ValidateMemo("text", "this memo is definitely way longer than twenty eight bytes limit"); err == nil {
		t.Fatalf("expected error for long text memo")
	}

	// ID
	if err := ValidateMemo("id", "1234567890"); err != nil {
		t.Fatalf("valid id memo failed: %v", err)
	}
	if err := ValidateMemo("id", "not-a-number"); err == nil {
		t.Fatalf("expected error for non-numeric id memo")
	}

	// Hash
	validHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := ValidateMemo("hash", validHash); err != nil {
		t.Fatalf("valid hash memo failed: %v", err)
	}
	if err := ValidateMemo("hash", "short-hash"); err == nil {
		t.Fatalf("expected error for short hash memo")
	}
	if err := ValidateMemo("hash", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789zzzzzz"); err == nil {
		t.Fatalf("expected error for non-hex hash memo")
	}

	// Return
	if err := ValidateMemo("return", validHash); err != nil {
		t.Fatalf("valid return memo failed: %v", err)
	}

	// Unknown
	if err := ValidateMemo("invalid_type", "data"); err == nil {
		t.Fatalf("expected error for unrecognized memo type")
	}
}

func TestFormatSARIF(t *testing.T) {
	rep := EvidenceReport{
		Status:         "FAIL",
		Reason:         "ledger movement divergence detected",
		LedgerSequence: 54321,
	}
	data, err := rep.FormatSARIF()
	if err != nil {
		t.Fatalf("FormatSARIF failed: %v", err)
	}
	sarifStr := string(data)
	if !strings.Contains(sarifStr, "sarif-schema-2.1.0.json") {
		t.Fatalf("missing sarif schema reference")
	}
	if !strings.Contains(sarifStr, `"version": "2.1.0"`) {
		t.Fatalf("missing sarif version")
	}
	if !strings.Contains(sarifStr, `"ruleId": "truthcheck/ledger-parity"`) {
		t.Fatalf("missing ruleId")
	}
	if !strings.Contains(sarifStr, `"level": "error"`) {
		t.Fatalf("expected error level for FAIL status")
	}
}
