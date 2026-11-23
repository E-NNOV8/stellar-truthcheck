package main

import (
	"testing"
)

func TestParseJSONLMovementsAndSacEventClassifier(t *testing.T) {
	jsonl := `
{"type":"transfer","from":"GAAA","to":"GBBB","amount":"15.1300000","asset_type":"credit_alphanum4","asset_code":"USDC","asset_issuer":"GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"}
{"type":"mint","from":"","to":"GBBB","amount":"100.0000000","asset_type":"native"}
`
	movements, err := ParseJSONLMovements(jsonl)
	if err != nil {
		t.Fatalf("ParseJSONLMovements failed: %v", err)
	}
	if len(movements) != 2 {
		t.Fatalf("expected 2 movements, got %d", len(movements))
	}
	if movements[0].AssetCode != "USDC" || movements[1].Type != "mint" {
		t.Fatalf("unexpected decoded movements: %+v", movements)
	}

	if _, err := ParseJSONLMovements("{bad-json"); err == nil {
		t.Fatalf("expected error on malformed JSONL")
	}

	for _, valid := range []string{"transfer", "mint", "burn", "clawback", "MINT"} {
		if _, err := ClassifySacEventType(valid); err != nil {
			t.Fatalf("expected %q to be valid SAC event: %v", valid, err)
		}
	}
	if _, err := ClassifySacEventType("approve_unknown"); err == nil {
		t.Fatalf("expected unsupported SAC topic to error")
	}
}
