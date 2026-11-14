package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// readLedgerInput converts storage representations, without altering ledger evidence.
func readLedgerInput(path string, raw []byte, format string, sequence uint, passphrase string) (xdr.LedgerCloseMeta, error) {
	var lcm xdr.LedgerCloseMeta
	if uint64(sequence) > uint64(^uint32(0)) {
		return lcm, errors.New("ledger sequence exceeds uint32")
	}
	switch format {
	case "base64":
		var err error
		lcm, err = readLedger(path, raw, passphrase)
		if err != nil {
			return lcm, err
		}
	case "xdr":
		if err := xdr.SafeUnmarshal(raw, &lcm); err != nil {
			return lcm, fmt.Errorf("decode raw LedgerCloseMeta: %w", err)
		}
	case "rpc":
		if sequence == 0 {
			return lcm, errors.New("rpc input requires --ledger-sequence; request getLedgers with xdrFormat=base64")
		}
		if err := rejectDuplicateJSONKeys(raw); err != nil {
			return lcm, fmt.Errorf("decode getLedgers response: %w", err)
		}
		var response struct {
			Error  json.RawMessage `json:"error"`
			Result struct {
				Ledgers []struct {
					Sequence uint32 `json:"sequence"`
					Hash     string `json:"hash"`
					Metadata string `json:"metadataXdr"`
				} `json:"ledgers"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return lcm, fmt.Errorf("decode getLedgers response: %w", err)
		}
		if len(response.Error) != 0 && string(response.Error) != "null" {
			return lcm, fmt.Errorf("RPC returned an error: %s; check provider access and ledger retention", response.Error)
		}
		matches := 0
		for _, ledger := range response.Result.Ledgers {
			if ledger.Sequence != uint32(sequence) {
				continue
			}
			matches++
			var err error
			lcm, err = readLedger(path, []byte(ledger.Metadata), passphrase)
			if err != nil {
				return lcm, err
			}
			if !strings.EqualFold(ledger.Hash, lcm.LedgerHash().HexString()) {
				return lcm, errors.New("RPC wrapper hash differs from metadata ledger hash")
			}
		}
		if matches != 1 {
			return lcm, fmt.Errorf("getLedgers response contains %d matches for ledger %d; obtain exactly one matching metadataXdr", matches, sequence)
		}
	case "galexie":
		if sequence == 0 {
			return lcm, errors.New("galexie input requires --ledger-sequence")
		}
		decoder, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(256<<20))
		if err != nil {
			return lcm, err
		}
		defer decoder.Close()
		decoded, err := decoder.DecodeAll(raw, nil)
		if err != nil {
			return lcm, fmt.Errorf("decompress Galexie zstd batch (256 MiB limit): %w", err)
		}
		var batch xdr.LedgerCloseMetaBatch
		if err := xdr.SafeUnmarshal(decoded, &batch); err != nil {
			return lcm, fmt.Errorf("decode LedgerCloseMetaBatch: %w", err)
		}
		lcm, err = batch.GetLedger(uint32(sequence))
		if err != nil {
			return lcm, err
		}
	default:
		return lcm, fmt.Errorf("unsupported --ledger-format %q; use base64, xdr, rpc, or galexie", format)
	}
	if lcm.V != 1 && lcm.V != 2 {
		return lcm, fmt.Errorf("unsupported LedgerCloseMeta version %d (supported: 1 or 2)", lcm.V)
	}
	if sequence != 0 && lcm.LedgerSequence() != uint32(sequence) {
		return lcm, fmt.Errorf("selected metadata ledger %d differs from --ledger-sequence %d", lcm.LedgerSequence(), sequence)
	}
	return lcm, nil
}

// prepareBundle adds identity context only. Movement claims always come from rowRaw.
func prepareBundle(lcm xdr.LedgerCloseMeta, rowRaw []byte, txHash string, opIndex int, passphrase string) ([]byte, error) {
	if len(txHash) != 64 {
		return nil, errors.New("transaction hash must be 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(txHash); err != nil {
		return nil, errors.New("transaction hash is not hexadecimal")
	}
	if opIndex < 0 || opIndex > 65534 {
		return nil, errors.New("provide --operation-index as a zero-based index (0 for first operation)")
	}
	if err := rejectDuplicateJSONKeys(rowRaw); err != nil {
		return nil, fmt.Errorf("decode operation output: %w", err)
	}
	var row struct {
		OperationOutput
		LedgerSequence uint32 `json:"ledger_sequence"`
		TransactionID  int64  `json:"transaction_id"`
		OperationID    int64  `json:"id"`
		Type           *int32 `json:"type"`
	}
	if err := json.Unmarshal(rowRaw, &row); err != nil {
		return nil, fmt.Errorf("decode one Stellar ETL operation object (not JSONL, array, or BigQuery row): %w", err)
	}
	if row.LedgerSequence != lcm.LedgerSequence() || row.Type == nil || *row.Type != 24 || row.TypeString != "invoke_host_function" {
		return nil, errors.New("operation output must have matching ledger_sequence, type=24, and type_string=invoke_host_function")
	}
	if row.Details.AssetBalanceChanges == nil {
		return nil, errors.New("operation output details.asset_balance_changes is missing or null; obtain supported transformer output; do not invent an empty list")
	}
	var rec *ETLRecord
	for i := 0; i < lcm.CountTransactions(); i++ {
		pair := lcm.TransactionResultPair(i)
		if strings.EqualFold(pair.TransactionHash.HexString(), txHash) {
			if rec != nil {
				return nil, errors.New("ambiguous canonical transaction hash")
			}
			rec = &ETLRecord{TransactionHash: strings.ToLower(txHash), TransactionIndex: uint32(i + 1), OperationIndex: int32(opIndex), OperationType: "OperationTypeInvokeHostFunction", TransactionSuccessful: boolPtr(pair.Successful()), FeeChargedStroops: fmt.Sprint(pair.Result.FeeCharged), OperationOutput: row.OperationOutput}
		}
	}
	if rec == nil {
		return nil, errors.New("transaction hash not found in ledger; check ledger, network, and fee-bump inner hash")
	}
	// Stellar ETL TOIDs: ledger << 32 | processing index << 12 | operation.
	if lcm.LedgerSequence() > 2147483647 || rec.TransactionIndex > 1048575 || opIndex > 4094 {
		return nil, errors.New("identity exceeds supported Stellar ETL TOID range")
	}
	txID := int64(lcm.LedgerSequence())<<32 | int64(rec.TransactionIndex)<<12
	if row.TransactionID != txID || row.OperationID != txID|int64(opIndex+1) {
		return nil, errors.New("ETL transaction_id or id does not match selected canonical transaction/operation; select the correct original row (keep integer IDs exact)")
	}
	bundle := ETLBundle{LedgerSequence: lcm.LedgerSequence(), Target: rec}
	if report := verify(lcm, bundle, strings.ToLower(txHash), passphrase); report.Status == "UNKNOWN" {
		return nil, fmt.Errorf("cannot prepare supported evidence: %s", report.Reason)
	}
	// Preserve the complete ETL object and its exact number tokens, including fields
	// outside the verifier's subset. Do not regenerate claims from canonical events.
	type preparedRecord struct {
		*ETLRecord
		Original json.RawMessage `json:"stellar_etl_operation_output"`
	}
	return json.MarshalIndent(struct {
		LedgerSequence uint32         `json:"ledger_sequence"`
		Target         preparedRecord `json:"target"`
	}{lcm.LedgerSequence(), preparedRecord{rec, json.RawMessage(rowRaw)}}, "", "  ")
}

func runPrepare(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("truthcheck prepare", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ledgerPath := fs.String("ledger-meta", "", "path to trusted complete ledger metadata")
	format := fs.String("ledger-format", "base64", "base64, xdr, rpc, or galexie")
	sequence := fs.Uint("ledger-sequence", 0, "ledger sequence (required for rpc or galexie)")
	rowPath := fs.String("operation-output", "", "one original Stellar ETL operation object JSON")
	hash := fs.String("transaction-hash", "", "canonical transaction hash (fee-bump: inner hash)")
	op := fs.Int("operation-index", -1, "zero-based operation index; required")
	passphrase := fs.String("network-passphrase", "", "Stellar network passphrase; required")
	fs.Usage = func() {
		fmt.Fprintln(stdout, "Usage: truthcheck prepare [options] > bundle.json\nSuccess emits an ETL bundle, not a verdict. Verify it using truthcheck --etl-output bundle.json.\nFailure emits UNKNOWN JSON and exits 3; do not use that output as a bundle.")
		fs.SetOutput(stdout)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return writeReport(stdout, unknown("invalid preparation options: "+err.Error()))
	}
	if fs.NArg() != 0 || *ledgerPath == "" || *rowPath == "" || strings.TrimSpace(*passphrase) == "" {
		return writeReport(stdout, unknown("provide --ledger-meta, --operation-output, --transaction-hash, --operation-index, and --network-passphrase; use prepare --help"))
	}
	ledgerRaw, err := os.ReadFile(*ledgerPath)
	if err != nil {
		return writeReport(stdout, unknown("read canonical ledger evidence: "+err.Error()))
	}
	lcm, err := readLedgerInput(*ledgerPath, ledgerRaw, *format, *sequence, *passphrase)
	if err != nil {
		return writeReport(stdout, unknown(err.Error()))
	}
	rowRaw, err := os.ReadFile(*rowPath)
	if err != nil {
		return writeReport(stdout, unknown("read operation output: "+err.Error()))
	}
	bundle, err := prepareBundle(lcm, rowRaw, *hash, *op, *passphrase)
	if err != nil {
		return writeReport(stdout, unknown(err.Error()))
	}
	if _, err := fmt.Fprintln(stdout, string(bundle)); err != nil {
		return 1
	}
	return 0
}
