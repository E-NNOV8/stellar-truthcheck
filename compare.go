package main

import "fmt"

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
