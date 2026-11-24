package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// EvidenceProvenance captures deterministic SHA-256 digests of the canonical
// ledger metadata and ETL export inputs used in an offline verification run.
type EvidenceProvenance struct {
	LedgerSequence   uint32 `json:"ledger_sequence"`
	CanonicalSHA256  string `json:"canonical_sha256"`
	ExportInputSHA256 string `json:"export_input_sha256"`
}

// ComputeEvidenceProvenance hashes the raw canonical and export byte slices.
func ComputeEvidenceProvenance(ledgerSeq uint32, canonicalBytes, exportBytes []byte) EvidenceProvenance {
	cSum := sha256.Sum256(canonicalBytes)
	eSum := sha256.Sum256(exportBytes)
	return EvidenceProvenance{
		LedgerSequence:    ledgerSeq,
		CanonicalSHA256:   hex.EncodeToString(cSum[:]),
		ExportInputSHA256: hex.EncodeToString(eSum[:]),
	}
}

// VerifyProvenanceMatch checks that recomputed digests match an expected provenance manifest.
func VerifyProvenanceMatch(expected, actual EvidenceProvenance) error {
	if expected.LedgerSequence != actual.LedgerSequence {
		return fmt.Errorf("ledger sequence mismatch: expected %d, got %d", expected.LedgerSequence, actual.LedgerSequence)
	}
	if expected.CanonicalSHA256 != actual.CanonicalSHA256 {
		return fmt.Errorf("canonical metadata sha256 mismatch: expected %s, got %s", expected.CanonicalSHA256, actual.CanonicalSHA256)
	}
	if expected.ExportInputSHA256 != actual.ExportInputSHA256 {
		return fmt.Errorf("export input sha256 mismatch: expected %s, got %s", expected.ExportInputSHA256, actual.ExportInputSHA256)
	}
	return nil
}
