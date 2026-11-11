package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

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
