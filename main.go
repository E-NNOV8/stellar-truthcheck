package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func run(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("truthcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ledgerPath := fs.String("ledger-meta", "", "path to base64 LedgerCloseMeta XDR")
	etlPath := fs.String("etl-output", "", "path to Stellar ETL operation output bundle JSON")
	passphrase := fs.String("network-passphrase", "", "network identity used to derive Stellar transaction hashes")
	txHash := fs.String("transaction-hash", "", "transaction hash to verify")
	etlRevision := fs.String("etl-source-revision", "", "provenance label only; not used for verdict")
	if err := fs.Parse(args); err != nil {
		return writeReport(stdout, unknown("invalid command options: "+err.Error()))
	}
	if *ledgerPath == "" || *etlPath == "" || *txHash == "" || *passphrase == "" {
		return writeReport(stdout, unknown("required evidence or identity input missing; provide --ledger-meta, --etl-output, --network-passphrase, and --transaction-hash"))
	}
	if len(*txHash) != 64 {
		return writeReport(stdout, unknown("transaction hash must be 64 hexadecimal characters"))
	}
	if _, err := hex.DecodeString(*txHash); err != nil {
		return writeReport(stdout, unknown("transaction hash is not hexadecimal"))
	}
	ledgerRaw, err := os.ReadFile(*ledgerPath)
	if err != nil {
		return writeReport(stdout, unknown("read canonical ledger evidence: "+err.Error()))
	}
	etlRaw, err := os.ReadFile(*etlPath)
	if err != nil {
		return writeReport(stdout, unknown("read ETL evidence: "+err.Error()))
	}
	lcm, err := readLedger(*ledgerPath, ledgerRaw, *passphrase)
	if err != nil {
		return writeReport(stdout, unknown(err.Error()))
	}
	bundle, err := decodeETL(*etlPath, etlRaw)
	if err != nil {
		return writeReport(stdout, unknown(err.Error()))
	}
	report := verify(lcm, bundle, strings.ToLower(*txHash), *passphrase)
	report.LedgerEvidencePath, _ = filepath.Abs(*ledgerPath)
	report.LedgerEvidenceSHA256 = sha256Hex(ledgerRaw)
	report.ETLEvidencePath, _ = filepath.Abs(*etlPath)
	report.ETLEvidenceSHA256 = sha256Hex(etlRaw)
	report.ETLSourceRevision = *etlRevision
	return writeReport(stdout, report)
}

func writeReport(w io.Writer, report EvidenceReport) int {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return 1
	}
	switch report.Status {
	case "PASS":
		return 0
	case "FAIL":
		return 2
	default:
		return 3
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
