package main

import (
	"fmt"
	"strings"
)

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

// MovementDiff captures the asymmetric set difference between canonical and ETL movements.
type MovementDiff struct {
	MissingInETL []ExportMovement `json:"missing_in_etl,omitempty"`
	SurplusInETL []ExportMovement `json:"surplus_in_etl,omitempty"`
}

// DiffMovements calculates the difference between canonical committed movements and ETL balance changes.
func DiffMovements(canonical, exported []ExportMovement) MovementDiff {
	canonCounts := make(map[movementKey]int)
	exportCounts := make(map[movementKey]int)
	canonMap := make(map[movementKey]ExportMovement)
	exportMap := make(map[movementKey]ExportMovement)

	for _, m := range canonical {
		k, err := makeMovementKey(m)
		if err == nil {
			canonCounts[k]++
			canonMap[k] = m
		}
	}
	for _, m := range exported {
		k, err := makeMovementKey(m)
		if err == nil {
			exportCounts[k]++
			exportMap[k] = m
		}
	}

	var diff MovementDiff
	for k, cCount := range canonCounts {
		eCount := exportCounts[k]
		if cCount > eCount {
			for i := 0; i < cCount-eCount; i++ {
				diff.MissingInETL = append(diff.MissingInETL, canonMap[k])
			}
		}
	}
	for k, eCount := range exportCounts {
		cCount := canonCounts[k]
		if eCount > cCount {
			for i := 0; i < eCount-cCount; i++ {
				diff.SurplusInETL = append(diff.SurplusInETL, exportMap[k])
			}
		}
	}
	return diff
}

// FilterMovements filters a slice of ExportMovement by asset code and/or issuer.
// An empty code or issuer matches all values for that attribute.
func FilterMovements(movements []ExportMovement, code, issuer string) []ExportMovement {
	if code == "" && issuer == "" {
		return movements
	}
	var filtered []ExportMovement
	for _, m := range movements {
		if code != "" && m.AssetCode != code {
			continue
		}
		if issuer != "" && m.AssetIssuer != issuer {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// TransferSummary aggregates total stroops volume and unique participant counts.
type TransferSummary struct {
	TotalStroops   int64 `json:"total_stroops"`
	MovementCount  int   `json:"movement_count"`
	UniqueAccounts int   `json:"unique_accounts"`
}

// SummarizeMovements computes aggregated transfer metrics for a slice of movements.
func SummarizeMovements(movements []ExportMovement) (TransferSummary, error) {
	accounts := make(map[string]struct{})
	var total int64
	for _, m := range movements {
		stroops, err := parseExportStroops(m.Amount)
		if err != nil {
			return TransferSummary{}, err
		}
		total += stroops
		if m.From != "" {
			accounts[m.From] = struct{}{}
		}
		if m.To != "" {
			accounts[m.To] = struct{}{}
		}
	}
	return TransferSummary{
		TotalStroops:   total,
		MovementCount:  len(movements),
		UniqueAccounts: len(accounts),
	}, nil
}

// ExportMovementsCSV formats movements as a CSV string.
func ExportMovementsCSV(movements []ExportMovement) string {
	var b strings.Builder
	b.WriteString("from,to,amount,asset_type,asset_code,asset_issuer\n")
	for _, m := range movements {
		fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s\n", m.From, m.To, m.Amount, m.AssetType, m.AssetCode, m.AssetIssuer)
	}
	return b.String()
}

// FilterMovementsByMinAmount returns only movements with amount >= minStroops.
func FilterMovementsByMinAmount(movements []ExportMovement, minStroops int64) []ExportMovement {
	if minStroops <= 0 {
		return movements
	}
	var out []ExportMovement
	for _, m := range movements {
		s, err := parseExportStroops(m.Amount)
		if err == nil && s >= minStroops {
			out = append(out, m)
		}
	}
	return out
}

// ValidateMemo verifies that a transaction memo complies with the Stellar memo specification.
func ValidateMemo(memoType, memoValue string) error {
	switch strings.ToLower(memoType) {
	case "", "none":
		if memoValue != "" {
			return fmt.Errorf("memo type %q must have empty value", memoType)
		}
		return nil
	case "text":
		if len(memoValue) > 28 {
			return fmt.Errorf("text memo exceeds max 28 bytes: got %d bytes", len(memoValue))
		}
		return nil
	case "id":
		var id uint64
		_, err := fmt.Sscanf(memoValue, "%d", &id)
		if err != nil {
			return fmt.Errorf("invalid id memo, expected unsigned integer: %w", err)
		}
		return nil
	case "hash", "return":
		if len(memoValue) != 64 {
			return fmt.Errorf("%s memo must be 64-char hex string (32 bytes), got %d chars", memoType, len(memoValue))
		}
		for _, c := range memoValue {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return fmt.Errorf("invalid hex character in %s memo: %c", memoType, c)
			}
		}
		return nil
	default:
		return fmt.Errorf("unrecognized memo type %q", memoType)
	}
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
