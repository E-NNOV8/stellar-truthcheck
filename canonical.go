package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
