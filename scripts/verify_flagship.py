#!/usr/bin/env python3
"""Run and independently assert Truthcheck's preserved real flagship cases."""

import base64
import json
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / "docs" / "evidence"
LEDGER = EVIDENCE / "ledger-64558277-metadata.xdr.base64"
ETL = EVIDENCE / "stellar-etl-62e06b1-ledger-64558277-reproduction.json"
PASSPHRASE = "Public Global Stellar Network ; September 2015"
ETL_REVISION = "62e06b18892c074b624f446c908169412970d75f"
TARGET = "575f16436ec1d6eabfc8f66e2dec553e61b53ed85914b50736c842a0d567266b"
CONTROL = "afbf759fa9317cc7a8e28812c8b5b0245e9d31e55e9e9d8cb6a72e5eb832d57d"


def invoke(binary: Path, tx_hash: str, ledger_path: Path = LEDGER):
    command = [
        str(binary),
        "--ledger-meta", str(ledger_path),
        "--etl-output", str(ETL),
        "--network-passphrase", PASSPHRASE,
        "--transaction-hash", tx_hash,
        "--etl-source-revision", ETL_REVISION,
    ]
    completed = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
    try:
        report = json.loads(completed.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError(
            f"CLI did not emit a JSON report (exit {completed.returncode}): "
            f"stdout={completed.stdout!r} stderr={completed.stderr!r}"
        ) from exc
    return completed, report


def require(condition: bool, message: str):
    if not condition:
        raise RuntimeError(message)


def verify_provenance_representations():
    aws_raw = (EVIDENCE / "aws-public-ledger-64558277.metadata.xdr").read_bytes()
    aws_b64 = base64.b64decode((EVIDENCE / "aws-public-ledger-64558277.metadata.xdr.base64").read_text().strip(), validate=True)
    onfinality_raw = (EVIDENCE / "onfinality-ledger-64558277.metadata.xdr").read_bytes()
    original_b64 = base64.b64decode((EVIDENCE / "ledger-64558277-metadata.xdr.base64").read_text().strip(), validate=True)
    wrapper = json.loads((EVIDENCE / "onfinality-getledgers-64558277.json").read_text())
    ledgers = wrapper.get("result", {}).get("ledgers", [])
    require(len(ledgers) == 1, "OnFinality wrapper must contain exactly one ledger")
    ledger = ledgers[0]
    wrapper_payload = base64.b64decode(ledger["metadataXdr"], validate=True)
    require(ledger.get("sequence") == 64558277, "OnFinality wrapper ledger sequence mismatch")
    require(ledger.get("hash") == "b276434d7e1654f2b32a09d43891c76122f6865b136ab98a030bc36ccf50e49b", "OnFinality wrapper ledger hash mismatch")
    require(aws_raw == aws_b64, "AWS raw XDR and base64 representations differ")
    require(aws_raw == onfinality_raw == original_b64 == wrapper_payload,
            "AWS and OnFinality extracted LedgerCloseMeta XDR bytes are not identical")


def main():
    if len(sys.argv) != 2:
        raise SystemExit("usage: python3 scripts/verify_flagship.py PATH_TO_TRUTHCHECK_BINARY")
    binary = Path(sys.argv[1]).resolve()
    require(binary.is_file(), f"Truthcheck binary does not exist: {binary}")
    require(LEDGER.is_file(), f"canonical fixture missing: {LEDGER}")
    require(ETL.is_file(), f"ETL fixture missing: {ETL}")
    verify_provenance_representations()

    failed, failed_report = invoke(binary, TARGET)
    require(failed.returncode == 2, f"target exit {failed.returncode}, expected 2; report={failed_report}")
    require(failed_report.get("status") == "FAIL", f"target status is not FAIL: {failed_report}")
    require(failed_report.get("transaction_hash", "").lower() == TARGET, "target transaction identity mismatch")
    require(failed_report.get("ledger_sequence") == 64558277, "target ledger identity mismatch")
    require(failed_report.get("transaction_success") is False, "canonical target was not identified as failed")
    require(failed_report.get("transaction_result") == "TransactionResultCodeTxFailed", "target result code mismatch")
    require(len(failed_report.get("etl_asset_balance_changes", [])) == 9, "target must contain the nine original ETL movements")
    require(not failed_report.get("canonical_committed_transfers"), "failed target unexpectedly has committed transfers")
    reason = failed_report.get("reason", "").lower()
    require("9 committed asset transfer" in reason and "transaction that failed" in reason and "diagnostic" in reason,
            f"target did not report the expected settlement contradiction: {reason}")

    passed, pass_report = invoke(binary, CONTROL)
    require(passed.returncode == 0, f"control exit {passed.returncode}, expected 0; report={pass_report}")
    require(pass_report.get("status") == "PASS", f"control status is not PASS: {pass_report}")
    require(pass_report.get("transaction_hash", "").lower() == CONTROL, "control transaction identity mismatch")
    require(pass_report.get("ledger_sequence") == 64558277, "control ledger identity mismatch")
    require(pass_report.get("transaction_success") is True, "canonical control was not identified as successful")
    canonical = pass_report.get("canonical_committed_transfers", [])
    exported = pass_report.get("etl_asset_balance_changes", [])
    require(len(canonical) == 1 and len(exported) == 1, "control must have exactly one canonical and exported movement")
    require(canonical[0] == exported[0], "control canonical and ETL movement records differ")
    movement = canonical[0]
    require(movement.get("type") == "transfer" and movement.get("asset_code") == "USDC", "control asset/type mismatch")
    require(movement.get("asset_issuer") == "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN", "control issuer mismatch")
    require(movement.get("amount") == "15.1300000", "control exact amount mismatch")
    require(movement.get("from") == "GCNOWLX3ICSI5JVM4M443Y2WGOTJROVQLRWOEXGOJ6GM6J4CLNJ4IEQA", "control sender mismatch")
    require(movement.get("to") == "CCDCTXGN7FPCGSPV4PEVNZODDNHUMDQ3SAKFHYW6TCCDK3JXSX4UVS3Y", "control recipient mismatch")

    with tempfile.TemporaryDirectory(prefix="truthcheck-missing-") as temp_dir:
        missing = Path(temp_dir) / "missing-ledger.xdr.base64"
        require(not missing.exists(), "UNKNOWN fixture path unexpectedly exists")
        unknown, unknown_report = invoke(binary, TARGET, missing)
    require(unknown.returncode == 3, f"missing-input exit {unknown.returncode}, expected 3; report={unknown_report}")
    require(unknown_report.get("status") == "UNKNOWN", f"missing evidence did not produce UNKNOWN: {unknown_report}")
    unknown_reason = unknown_report.get("reason", "").lower()
    require("read canonical ledger evidence" in unknown_reason and "no such file" in unknown_reason,
            f"missing-evidence diagnostic is not actionable: {unknown_reason}")

    print("PROVENANCE: AWS XDR, AWS base64, OnFinality XDR, and OnFinality JSON metadataXdr are byte-identical")
    print(f"FAIL exit={failed.returncode}: target {TARGET}; canonical failed; 9 ETL movements; no committed transfers")
    print(f"PASS exit={passed.returncode}: control {CONTROL}; exact 15.1300000 USDC committed/exported movement match")
    print(f"UNKNOWN exit={unknown.returncode}: missing canonical input; explicit read-evidence diagnostic")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"flagship verification failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
