package main

import "testing"

func TestEvidenceProvenanceHashingAndVerification(t *testing.T) {
	p1 := ComputeEvidenceProvenance(64558277, []byte("canonical-xdr-bytes"), []byte("etl-json-bytes"))
	p2 := ComputeEvidenceProvenance(64558277, []byte("canonical-xdr-bytes"), []byte("etl-json-bytes"))

	if len(p1.CanonicalSHA256) != 64 || len(p1.ExportInputSHA256) != 64 {
		t.Fatalf("expected 64-char sha256 hex digests, got %+v", p1)
	}
	if err := VerifyProvenanceMatch(p1, p2); err != nil {
		t.Fatalf("identical evidence should verify cleanly: %v", err)
	}

	tampered := ComputeEvidenceProvenance(64558277, []byte("canonical-xdr-bytes"), []byte("tampered-etl"))
	if err := VerifyProvenanceMatch(p1, tampered); err == nil {
		t.Fatalf("expected provenance mismatch on tampered export input")
	}
}
