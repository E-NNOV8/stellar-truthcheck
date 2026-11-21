# Stellar TruthCheck Offline Verification Guide

`stellar-truthcheck` independently proves whether Stellar ETL exports match canonical Core ledger transaction executions bit-for-bit without trusted intermediaries.

---

## 1. Input Preparation & Formats

TruthCheck accepts two primary input bundles:
1. **Canonical Core Ledger Snapshot**: XDR-encoded ledger close meta containing transactions and operation results.
2. **Stellar ETL Balance Changes**: Line-delimited or bundled JSON representing asset balance changes and fees.

---

## 2. CLI Usage

```bash
# Verify canonical ledger input against ETL export
stellar-truthcheck verify --canonical ledger_1234.xdr --etl bundle_1234.json

# Export markdown scorecard
stellar-truthcheck verify --canonical ledger_1234.xdr --etl bundle_1234.json --format markdown > scorecard.md
```
