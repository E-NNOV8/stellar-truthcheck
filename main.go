package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"
)

const sdkVersion = "github.com/stellar/go-stellar-sdk v0.8.0"

type ExportMovement struct {
	Amount      string `json:"amount"`
	AssetCode   string `json:"asset_code"`
	AssetIssuer string `json:"asset_issuer"`
	AssetType   string `json:"asset_type"`
	From        string `json:"from"`
	To          string `json:"to"`
	Type        string `json:"type"`
}

type OperationOutput struct {
	TypeString string `json:"type_string"`
	Details    struct {
		AssetBalanceChanges *[]ExportMovement `json:"asset_balance_changes"`
	} `json:"details"`
}

type ETLRecord struct {
	TransactionHash       string          `json:"transaction_hash"`
	TransactionIndex      uint32          `json:"transaction_index"`
	OperationIndex        int32           `json:"operation_index"`
	OperationType         string          `json:"operation_type"`
	TransactionSuccessful *bool           `json:"transaction_successful"`
	FeeChargedStroops     string          `json:"fee_charged_stroops"`
	OperationOutput       OperationOutput `json:"stellar_etl_operation_output"`
}

type ETLBundle struct {
	LedgerSequence uint32     `json:"ledger_sequence"`
	Target         *ETLRecord `json:"target"`
	Control        *ETLRecord `json:"control"`
}

type movementKey struct {
	assetType string
	assetCode string
	issuer    string
	from      string
	to        string
	stroops   int64
}

type EvidenceReport struct {
	Status               string           `json:"status"`
	Reason               string           `json:"reason"`
	NetworkPassphrase    string           `json:"network_passphrase,omitempty"`
	LedgerSequence       uint32           `json:"ledger_sequence,omitempty"`
	LedgerHash           string           `json:"ledger_hash,omitempty"`
	TransactionHash      string           `json:"transaction_hash,omitempty"`
	TransactionIndex     uint32           `json:"transaction_index,omitempty"`
	OperationIndex       int32            `json:"operation_index,omitempty"`
	TransactionSuccess   *bool            `json:"transaction_success,omitempty"`
	TransactionResult    string           `json:"transaction_result,omitempty"`
	FeeChargedStroops    string           `json:"fee_charged_stroops,omitempty"`
	CanonicalMovements   []ExportMovement `json:"canonical_committed_transfers,omitempty"`
	ExportMovements      []ExportMovement `json:"etl_asset_balance_changes,omitempty"`
	LedgerEvidencePath   string           `json:"ledger_evidence_path,omitempty"`
	LedgerEvidenceSHA256 string           `json:"ledger_evidence_sha256,omitempty"`
	ETLEvidencePath      string           `json:"etl_evidence_path,omitempty"`
	ETLEvidenceSHA256    string           `json:"etl_evidence_sha256,omitempty"`
	ETLSourceRevision    string           `json:"etl_source_revision,omitempty"`
	SDK                  string           `json:"sdk,omitempty"`
}

func unknown(reason string) EvidenceReport { return EvidenceReport{Status: "UNKNOWN", Reason: reason} }

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// parseExportStroops converts a decimal ETL amount to exact integer stroops.
// It rejects rounding, exponents, signs, zero, and values outside int64.
func parseExportStroops(s string) (int64, error) {
	if s == "" || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") || strings.ContainsAny(s, "eE") {
		return 0, fmt.Errorf("invalid decimal amount %q", s)
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("invalid decimal amount %q", s)
	}
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid decimal amount %q", s)
		}
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
		if frac == "" || len(frac) > 7 {
			return 0, fmt.Errorf("amount must have 1 to 7 fractional digits: %q", s)
		}
		for _, c := range frac {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("invalid decimal amount %q", s)
			}
		}
	}
	frac += strings.Repeat("0", 7-len(frac))
	whole, ok := new(big.Int).SetString(parts[0], 10)
	if !ok {
		return 0, fmt.Errorf("invalid decimal amount %q", s)
	}
	whole.Mul(whole, big.NewInt(10_000_000))
	if frac != "" {
		fraction, ok := new(big.Int).SetString(frac, 10)
		if !ok {
			return 0, fmt.Errorf("invalid decimal amount %q", s)
		}
		whole.Add(whole, fraction)
	}
	if !whole.IsInt64() || whole.Sign() <= 0 {
		return 0, fmt.Errorf("amount is zero or exceeds supported stroop range: %q", s)
	}
	return whole.Int64(), nil
}

func canonicalStroops(raw string) (int64, error) {
	v, ok := new(big.Int).SetString(raw, 10)
	if !ok || !v.IsInt64() || v.Sign() <= 0 {
		return 0, fmt.Errorf("unsupported canonical raw amount %q", raw)
	}
	return v.Int64(), nil
}

func makeMovementKey(m ExportMovement) (movementKey, error) {
	if m.Type != "transfer" {
		return movementKey{}, fmt.Errorf("unsupported movement type %q", m.Type)
	}
	if m.From == "" || m.To == "" {
		return movementKey{}, errors.New("transfer is missing source or destination")
	}
	k := movementKey{assetType: m.AssetType, assetCode: m.AssetCode, issuer: m.AssetIssuer, from: m.From, to: m.To}
	if m.AssetType == "native" {
		if m.AssetCode != "" || m.AssetIssuer != "" {
			return movementKey{}, errors.New("native transfer unexpectedly has credit-asset identity")
		}
	} else if m.AssetType == "credit_alphanum4" || m.AssetType == "credit_alphanum12" {
		if m.AssetCode == "" || m.AssetIssuer == "" {
			return movementKey{}, errors.New("credit transfer is missing asset code or issuer")
		}
	} else {
		return movementKey{}, fmt.Errorf("unsupported asset type %q", m.AssetType)
	}
	amount, err := parseExportStroops(m.Amount)
	if err != nil {
		return movementKey{}, err
	}
	k.stroops = amount
	return k, nil
}

func formatStroops(amount int64) string {
	return fmt.Sprintf("%d.%07d", amount/10_000_000, amount%10_000_000)
}

func scanJSONValue(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("malformed JSON object")
		}
	case '[':
		for dec.More() {
			if err := scanJSONValue(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("malformed JSON array")
		}
	}
	return nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := scanJSONValue(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); err == nil {
		return errors.New("multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func decodeETL(path string, raw []byte) (ETLBundle, error) {
	var b ETLBundle
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return b, fmt.Errorf("decode ETL evidence %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return b, fmt.Errorf("decode ETL evidence %s: %w", path, err)
	}
	if b.LedgerSequence == 0 {
		return b, errors.New("ETL evidence has no ledger_sequence")
	}
	return b, nil
}

func readLedger(path string, raw []byte, passphrase string) (xdr.LedgerCloseMeta, error) {
	var lcm xdr.LedgerCloseMeta
	if strings.TrimSpace(passphrase) == "" {
		return lcm, errors.New("network passphrase is required")
	}
	if err := xdr.SafeUnmarshalBase64(strings.TrimSpace(string(raw)), &lcm); err != nil {
		return lcm, fmt.Errorf("decode LedgerCloseMeta XDR from %s: %w", path, err)
	}
	if lcm.V != 1 && lcm.V != 2 {
		return lcm, fmt.Errorf("unsupported LedgerCloseMeta version %d (supported: 1 or 2)", lcm.V)
	}
	return lcm, nil
}

func selectETLRecord(b ETLBundle, txHash string) (ETLRecord, error) {
	var matches []ETLRecord
	for _, rec := range []*ETLRecord{b.Target, b.Control} {
		if rec != nil && strings.EqualFold(rec.TransactionHash, txHash) {
			matches = append(matches, *rec)
		}
	}
	if len(matches) == 0 {
		return ETLRecord{}, errors.New("matching ETL operation record is missing")
	}
	if len(matches) != 1 {
		return ETLRecord{}, errors.New("multiple ETL records have the requested transaction hash")
	}
	return matches[0], nil
}

func committedTransfers(meta xdr.TransactionMeta, opIndex int32, expectedOperationCount int, passphrase string, successful bool) ([]ExportMovement, error) {
	if meta.V != 4 || meta.V4 == nil {
		return nil, fmt.Errorf("unsupported TransactionMeta version %d (only V4 is implemented)", meta.V)
	}
	ops := meta.V4.Operations
	if !successful {
		if len(ops) != 0 || len(meta.V4.TxChangesAfter) != 0 {
			return nil, errors.New("failed transaction has unexpected operation or post-apply metadata")
		}
		return []ExportMovement{}, nil
	}
	if len(ops) != expectedOperationCount {
		return nil, fmt.Errorf("incomplete operation metadata: canonical has %d entries for %d transaction operations", len(ops), expectedOperationCount)
	}
	if opIndex < 0 || int(opIndex) >= len(ops) {
		return nil, errors.New("operation metadata missing for selected operation")
	}
	var movements []ExportMovement
	for _, ev := range ops[opIndex].Events {
		if ev.Type != xdr.ContractEventTypeContract || ev.Body.V != 0 || ev.Body.V0 == nil {
			continue
		}
		topics := ev.Body.V0.Topics
		if len(topics) == 0 {
			continue
		}
		name, isSymbol := topics[0].GetSym()
		if !isSymbol || string(name) != "transfer" {
			continue
		}
		if ev.ContractId == nil || len(topics) != 4 || ev.Body.V0.Data.Type != xdr.ScValTypeScvI128 {
			return nil, errors.New("unsupported or malformed contract transfer event")
		}
		fromAddr, okFrom := topics[1].GetAddress()
		toAddr, okTo := topics[2].GetAddress()
		assetLabel, okAsset := topics[3].GetStr()
		if !okFrom || !okTo || !okAsset {
			return nil, errors.New("contract transfer event has unsupported topic types")
		}
		assets, err := xdr.BuildAssets(string(assetLabel))
		if err != nil || len(assets) != 1 {
			return nil, errors.New("contract transfer asset label is invalid or ambiguous")
		}
		asset := assets[0]
		contractID, err := asset.ContractID(passphrase)
		if err != nil || !strings.EqualFold(hex.EncodeToString(contractID[:]), hex.EncodeToString(ev.ContractId[:])) {
			return nil, errors.New("contract transfer is not verified as the Stellar Asset Contract for its declared asset")
		}
		from, err := fromAddr.String()
		if err != nil {
			return nil, fmt.Errorf("encode transfer source: %w", err)
		}
		to, err := toAddr.String()
		if err != nil {
			return nil, fmt.Errorf("encode transfer destination: %w", err)
		}
		amount, err := canonicalStroops(ev.Body.V0.Data.String())
		if err != nil {
			return nil, err
		}
		m := ExportMovement{Type: "transfer", From: from, To: to, Amount: formatStroops(amount)}
		switch asset.Type {
		case xdr.AssetTypeAssetTypeNative:
			m.AssetType = "native"
		case xdr.AssetTypeAssetTypeCreditAlphanum4:
			m.AssetType, m.AssetCode, m.AssetIssuer = "credit_alphanum4", asset.GetCode(), asset.GetIssuer()
		case xdr.AssetTypeAssetTypeCreditAlphanum12:
			m.AssetType, m.AssetCode, m.AssetIssuer = "credit_alphanum12", asset.GetCode(), asset.GetIssuer()
		default:
			return nil, fmt.Errorf("unsupported canonical asset type %s", asset.Type)
		}
		movements = append(movements, m)
	}
	return movements, nil
}

func sameCounts(a, b map[movementKey]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, n := range a {
		if b[k] != n {
			return false
		}
	}
	return true
}

func compareTransfers(successful bool, canonical, exported []ExportMovement) EvidenceReport {
	for _, m := range exported {
		if _, err := makeMovementKey(m); err != nil {
			return unknown("unsupported or malformed ETL movement: " + err.Error())
		}
	}
	for _, m := range canonical {
		if _, err := makeMovementKey(m); err != nil {
			return unknown("unsupported canonical movement: " + err.Error())
		}
	}
	if !successful && len(exported) > 0 {
		return EvidenceReport{Status: "FAIL", Reason: fmt.Sprintf("ETL claims %d committed asset transfer(s) for a transaction that failed; diagnostic contract events do not establish committed movement", len(exported))}
	}
	a, b := make(map[movementKey]int), make(map[movementKey]int)
	for _, m := range canonical {
		k, _ := makeMovementKey(m)
		a[k]++
	}
	for _, m := range exported {
		k, _ := makeMovementKey(m)
		b[k]++
	}
	if !sameCounts(a, b) {
		return EvidenceReport{Status: "FAIL", Reason: "ETL asset_balance_changes differ from committed transfers in canonical operation metadata"}
	}
	return EvidenceReport{Status: "PASS", Reason: "ETL transfer records match committed transfers in canonical operation metadata; transaction fees are separate"}
}

func verifyLedgerCommitments(lcm xdr.LedgerCloseMeta, header xdr.LedgerHeader) error {
	var txSet xdr.GeneralizedTransactionSet
	switch lcm.V {
	case 1:
		txSet = lcm.MustV1().TxSet
	case 2:
		txSet = lcm.MustV2().TxSet
	default:
		return fmt.Errorf("unsupported LedgerCloseMeta version %d", lcm.V)
	}
	txSetHash, err := xdr.HashXdr(&txSet)
	if err != nil {
		return fmt.Errorf("hash generalized transaction set: %w", err)
	}
	if txSetHash != header.ScpValue.TxSetHash {
		return errors.New("generalized transaction set hash does not match the ledger header commitment")
	}
	results := make([]xdr.TransactionResultPair, lcm.CountTransactions())
	for i := range results {
		results[i] = lcm.TransactionResultPair(i)
	}
	resultSetHash, err := xdr.HashXdr(&xdr.TransactionResultSet{Results: results})
	if err != nil {
		return fmt.Errorf("hash transaction result set: %w", err)
	}
	if resultSetHash != header.TxSetResultHash {
		return errors.New("transaction result set hash does not match the ledger header commitment")
	}
	return nil
}

func verify(lcm xdr.LedgerCloseMeta, etl ETLBundle, txHash, passphrase string) EvidenceReport {
	report := EvidenceReport{Status: "UNKNOWN", NetworkPassphrase: passphrase, LedgerSequence: lcm.LedgerSequence(), SDK: sdkVersion}
	if lcm.LedgerSequence() != etl.LedgerSequence {
		report.Reason = fmt.Sprintf("ledger identity mismatch: canonical %d, ETL %d", lcm.LedgerSequence(), etl.LedgerSequence)
		return report
	}
	header := lcm.LedgerHeaderHistoryEntry().Header
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		report.Reason = "encode canonical ledger header for identity check: " + err.Error()
		return report
	}
	computedLedgerHash := sha256.Sum256(headerBytes)
	if !strings.EqualFold(hex.EncodeToString(computedLedgerHash[:]), lcm.LedgerHash().HexString()) {
		report.Reason = "canonical ledger header hash does not match LedgerCloseMeta ledger hash"
		return report
	}
	if err := verifyLedgerCommitments(lcm, header); err != nil {
		report.Reason = "canonical ledger contents are not committed by the verified header: " + err.Error()
		return report
	}
	report.LedgerHash = lcm.LedgerHash().HexString()
	envelopes := lcm.TransactionEnvelopes()
	if len(envelopes) != lcm.CountTransactions() {
		report.Reason = "ledger envelope and result counts differ"
		return report
	}
	envelopeByHash := make(map[string]xdr.TransactionEnvelope, len(envelopes))
	for _, env := range envelopes {
		computed, hashErr := network.HashTransactionInEnvelope(env, passphrase)
		if hashErr != nil {
			report.Reason = "derive transaction hash using supplied network passphrase: " + hashErr.Error()
			return report
		}
		key := hex.EncodeToString(computed[:])
		if _, exists := envelopeByHash[key]; exists {
			report.Reason = "duplicate canonical transaction envelope hash"
			return report
		}
		envelopeByHash[key] = env
	}
	found := -1
	var selectedEnvelope xdr.TransactionEnvelope
	for i := 0; i < lcm.CountTransactions(); i++ {
		pair := lcm.TransactionResultPair(i)
		key := strings.ToLower(pair.TransactionHash.HexString())
		env, exists := envelopeByHash[key]
		if !exists {
			report.Reason = fmt.Sprintf("no canonical transaction envelope matches result hash at processing index %d", i+1)
			return report
		}
		if strings.EqualFold(key, txHash) {
			if found >= 0 {
				report.Reason = "canonical transaction hash is ambiguous in ledger"
				return report
			}
			found = i
			selectedEnvelope = env
		}
	}
	if found < 0 {
		report.Reason = "transaction hash not found in canonical ledger under supplied network passphrase"
		return report
	}
	pair := lcm.TransactionResultPair(found)
	report.TransactionHash = pair.TransactionHash.HexString()
	report.TransactionIndex = uint32(found + 1)
	successful := pair.Successful()
	report.TransactionSuccess = boolPtr(successful)
	report.TransactionResult = pair.Result.Result.Code.String()
	report.FeeChargedStroops = fmt.Sprintf("%d", pair.Result.FeeCharged)

	rec, err := selectETLRecord(etl, txHash)
	if err != nil {
		report.Reason = err.Error()
		return report
	}
	if rec.TransactionIndex != report.TransactionIndex || rec.TransactionIndex == 0 {
		report.Reason = fmt.Sprintf("transaction index mismatch or missing: canonical %d, ETL %d", report.TransactionIndex, rec.TransactionIndex)
		return report
	}
	if rec.OperationIndex < 0 || int(rec.OperationIndex) >= len(selectedEnvelope.Operations()) {
		report.Reason = "ETL operation index is outside the canonical transaction"
		return report
	}
	if rec.OperationType != "OperationTypeInvokeHostFunction" || rec.OperationOutput.TypeString != "invoke_host_function" || selectedEnvelope.Operations()[rec.OperationIndex].Body.Type != xdr.OperationTypeInvokeHostFunction {
		report.Reason = "unsupported or inconsistent ETL operation identity"
		return report
	}
	if rec.TransactionSuccessful == nil || *rec.TransactionSuccessful != successful {
		report.Reason = "ETL envelope success metadata is missing or conflicts with canonical result"
		return report
	}
	if rec.OperationOutput.Details.AssetBalanceChanges == nil {
		report.Reason = "ETL asset_balance_changes field is missing or null"
		return report
	}
	exported := *rec.OperationOutput.Details.AssetBalanceChanges
	report.OperationIndex = rec.OperationIndex
	report.ExportMovements = exported
	if rec.FeeChargedStroops != "" && rec.FeeChargedStroops != report.FeeChargedStroops {
		report.Reason = "ETL fee metadata conflicts with canonical fee result"
		return report
	}
	meta := lcm.TxApplyProcessing(found)
	canonical, metaErr := committedTransfers(meta, rec.OperationIndex, len(selectedEnvelope.Operations()), passphrase, successful)
	if metaErr != nil {
		report.Reason = "canonical transaction metadata is incomplete or unsupported: " + metaErr.Error()
		return report
	}
	report.CanonicalMovements = canonical
	comparison := compareTransfers(successful, canonical, exported)
	report.Status, report.Reason = comparison.Status, comparison.Reason
	return report
}

func boolPtr(v bool) *bool { return &v }

func run(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("truthcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ledgerPath := fs.String("ledger-meta", "", "path to base64 LedgerCloseMeta XDR")
	etlPath := fs.String("etl-output", "", "path to Stellar ETL operation output bundle JSON")
	passphrase := fs.String("network-passphrase", "", "network identity used to derive Stellar transaction hashes")
	txHash := fs.String("transaction-hash", "", "transaction hash to verify")
	etlRevision := fs.String("etl-source-revision", "", "provenance label only; not used for verdict")
	if err := fs.Parse(args); err != nil {
		return writeReport(stdout, unknown("invalid command options: "+err.Error()))
	}
	if *ledgerPath == "" || *etlPath == "" || *txHash == "" || *passphrase == "" {
		return writeReport(stdout, unknown("required evidence or identity input missing; provide --ledger-meta, --etl-output, --network-passphrase, and --transaction-hash"))
	}
	if len(*txHash) != 64 {
		return writeReport(stdout, unknown("transaction hash must be 64 hexadecimal characters"))
	}
	if _, err := hex.DecodeString(*txHash); err != nil {
		return writeReport(stdout, unknown("transaction hash is not hexadecimal"))
	}
	ledgerRaw, err := os.ReadFile(*ledgerPath)
	if err != nil {
		return writeReport(stdout, unknown("read canonical ledger evidence: "+err.Error()))
	}
	etlRaw, err := os.ReadFile(*etlPath)
	if err != nil {
		return writeReport(stdout, unknown("read ETL evidence: "+err.Error()))
	}
	lcm, err := readLedger(*ledgerPath, ledgerRaw, *passphrase)
	if err != nil {
		return writeReport(stdout, unknown(err.Error()))
	}
	bundle, err := decodeETL(*etlPath, etlRaw)
	if err != nil {
		return writeReport(stdout, unknown(err.Error()))
	}
	report := verify(lcm, bundle, strings.ToLower(*txHash), *passphrase)
	report.LedgerEvidencePath, _ = filepath.Abs(*ledgerPath)
	report.LedgerEvidenceSHA256 = sha256Hex(ledgerRaw)
	report.ETLEvidencePath, _ = filepath.Abs(*etlPath)
	report.ETLEvidenceSHA256 = sha256Hex(etlRaw)
	report.ETLSourceRevision = *etlRevision
	return writeReport(stdout, report)
}

func writeReport(w io.Writer, report EvidenceReport) int {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return 1
	}
	switch report.Status {
	case "PASS":
		return 0
	case "FAIL":
		return 2
	default:
		return 3
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
