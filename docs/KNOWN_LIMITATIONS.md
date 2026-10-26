# Known limitations

Truthcheck is a narrow offline verifier, not a general Stellar reconciliation system.

## Supported today

- `LedgerCloseMeta` versions 1 and 2.
- `TransactionMeta` version 4.
- The preserved Stellar ETL invoke-host-function operation-output shape.
- Stellar Asset Contract `transfer` events in the implemented topics/data form.
- Native, credit-alphanum4, and credit-alphanum12 identities where supported by the event parser.
- Positive exact integer stroop amounts within the current signed-64-bit comparison range.

## Not supported or not established

- Actual Hubble/BigQuery production rows; the exact historical Hubble row is unavailable.
- General operation/effect coverage, classic operations, arbitrary contract events, mint/burn or every SAC behavior.
- Other ETL schemas, indexers, or arbitrary cross-indexer compatibility.
- Metadata versions/events outside the supported shapes. Unsupported or incomplete evidence must produce UNKNOWN or an explicit error, not PASS.
- Cryptographic authentication of apply-metadata bytes against Stellar consensus, SCP-quorum verification, or independent Core re-execution.
- Automatic ledger retrieval, Hubble/BigQuery access, network querying, or production pipeline integration.
- Operator adoption or recurring demand validation.

The caller must supply complete ledger-close metadata from a trusted source. The ledger header commits the transaction set and results but not the apply-metadata event bytes directly. The AWS and OnFinality copies matched byte-for-byte for the preserved ledger, but may share upstream data or infrastructure.

The selected license is Apache-2.0, matching WeighIn. The Go module path is `github.com/E-NNOV8/stellar-truthcheck`. The public GitHub Actions flagship proof passed for revision `18fe9f468ab990d87e8b694587f2488cf0e89a01`; that result does not establish CI status for later revisions.
