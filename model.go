package main

import (
	"encoding/json"
	"fmt"
	"strings"
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

// ToJSON serializes the EvidenceReport to indented JSON.
func (r EvidenceReport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// IsPass returns true if the verification status is PASS.
func (r EvidenceReport) IsPass() bool {
	return r.Status == "PASS"
}

// IsFail returns true if the verification status is FAIL.
func (r EvidenceReport) IsFail() bool {
	return r.Status == "FAIL"
}

// FormatMarkdownScorecard formats the verification report into a markdown summary scorecard.
func (r EvidenceReport) FormatMarkdownScorecard() string {
	var sb strings.Builder
	sb.WriteString("# Stellar TruthCheck Verification Scorecard\n\n")
	sb.WriteString(fmt.Sprintf("- **Status**: %s\n", r.Status))
	if r.Reason != "" {
		sb.WriteString(fmt.Sprintf("- **Reason**: %s\n", r.Reason))
	}
	sb.WriteString(fmt.Sprintf("- **Ledger Sequence**: %d\n", r.LedgerSequence))
	sb.WriteString(fmt.Sprintf("- **Canonical Transfers**: %d\n", len(r.CanonicalMovements)))
	sb.WriteString(fmt.Sprintf("- **ETL Balance Changes**: %d\n", len(r.ExportMovements)))
	return sb.String()
}
