package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func run(args []string, stdout io.Writer) int {
	if len(args) > 0 && args[0] == "prepare" {
		return runPrepare(args[1:], stdout)
	}
	fs := flag.NewFlagSet("truthcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ledgerPath := fs.String("ledger-meta", "", "path to base64 LedgerCloseMeta XDR")
	ledgerFormat := fs.String("ledger-format", "base64", "ledger encoding: base64, xdr, rpc, or galexie (zstd batch)")
	ledgerSequence := fs.Uint("ledger-sequence", 0, "required ledger sequence for rpc or galexie input")
	etlPath := fs.String("etl-output", "", "path to Stellar ETL operation output bundle JSON")
	passphrase := fs.String("network-passphrase", "", "network identity used to derive Stellar transaction hashes")
	txHash := fs.String("transaction-hash", "", "transaction hash to verify")
	etlRevision := fs.String("etl-source-revision", "", "provenance label only; not used for verdict")
	threshold := fs.Int64("threshold", 0, "optional minimum transfer amount in stroops to include in verified movements")
	verbose := fs.Bool("verbose", false, "log step-by-step transfer comparison details")
	fs.BoolVar(verbose, "v", false, "shorthand for -verbose")
	fs.Usage = func() {
		fmt.Fprintln(stdout, "Usage: truthcheck [options]\n       truthcheck prepare --help\nOffline supported SAC transfer comparison. See docs/WORKFLOW.md for input sources and preparation.")
		fs.SetOutput(stdout)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return writeReport(stdout, unknown("invalid command options: "+err.Error()))
	}
	if fs.NArg() != 0 {
		return writeReport(stdout, unknown("unexpected positional arguments; use --help"))
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
	lcm, err := readLedgerInput(*ledgerPath, ledgerRaw, *ledgerFormat, *ledgerSequence, *passphrase)
	if err != nil {
		return writeReport(stdout, unknown(err.Error()))
	}
	bundle, err := decodeETL(*etlPath, etlRaw)
	if err != nil {
		return writeReport(stdout, unknown(err.Error()))
	}
	if bundle.Target == nil && bundle.Control == nil {
		return writeReport(stdout, unknown("ETL bundle has no target or control record; for an original Stellar ETL operation object, use truthcheck prepare --help"))
	}
	report := verify(lcm, bundle, strings.ToLower(*txHash), *passphrase)
	if *threshold > 0 {
		report.CanonicalMovements = FilterMovementsByMinAmount(report.CanonicalMovements, *threshold)
		report.ExportMovements = FilterMovementsByMinAmount(report.ExportMovements, *threshold)
	}
	if *verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Verification complete: status=%s canonical=%d etl=%d\n", report.Status, len(report.CanonicalMovements), len(report.ExportMovements))
	}
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
