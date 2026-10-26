# Architecture and trust boundary

## Data flow

```text
Base64 LedgerCloseMeta XDR ──> official Stellar Go SDK decode
                                  │
                                  ├─ verify ledger-header hash
                                  ├─ verify tx-set and result-set commitments
                                  ├─ derive transaction hashes with network passphrase
                                  └─ read canonical transaction result and supported operation metadata

Stellar ETL operation JSON ───────> validate transaction/operation identity and movement fields

Canonical committed SAC transfers + ETL movement claims
                                  └─ exact multiset comparison ──> PASS / FAIL / UNKNOWN
```

The CLI lives in `main.go`; behavior and adversarial cases are in `main_test.go`. It uses `github.com/stellar/go-stellar-sdk v0.8.0`. Amounts are converted to integer stroops; floating-point comparison is not used.

## Identity and execution semantics

The verifier derives transaction hashes from canonical envelopes using the provided network passphrase and joins them to transaction result pairs by hash. It checks the ledger sequence against the ETL bundle, verifies the canonical ledger-header hash, then checks the generalized transaction-set and transaction-result-set hashes against header commitments.

Transaction-level success comes from the transaction result pair. On the supported successful path, movements are extracted from the selected operation's committed `TransactionMeta` v4 contract events and validated as Stellar Asset Contract transfers. Diagnostic events are not treated as committed execution evidence. Fees are reported separately from token movements.

The ETL operation record is an assertion to compare, not the canonical oracle. The parser for canonical transfer evidence is implemented using the official Stellar XDR/SDK types and is independent of the Stellar ETL transformation code.

## Verdict meaning

- PASS: supported movement claims match the supported committed movements.
- FAIL: supported claimed movements contradict canonical execution or movement evidence.
- UNKNOWN: required evidence or interpretation is incomplete, malformed, ambiguous, or unsupported.

The checker is deliberately not a complete ledger auditor. The supported formats and semantic limits are listed in [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md).

## Input trust

The ledger header binds its transaction set and result set, but not the apply-metadata event bytes directly. Truthcheck checks internal header commitments and interprets supplied metadata; it does not authenticate the archive provider, verify an SCP quorum, or re-execute Stellar Core. Use complete ledger metadata from a source you trust. See [EVIDENCE.md](EVIDENCE.md) for the independent-source comparison and its limits.
