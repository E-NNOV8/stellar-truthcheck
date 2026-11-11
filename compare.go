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
