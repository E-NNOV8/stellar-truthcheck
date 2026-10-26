# Preserved flagship input files

These files are public ledger data and the source-reproduced output of the pinned Stellar ETL transformer. They contain no credentials. Provenance, representation-specific checksums, and the limits of the comparison are documented in `../EVIDENCE.md`.

- `aws-public-ledger-64558277.xdr.zst`: original public AWS object (compressed batch source).
- `aws-public-ledger-64558277.metadata.xdr`: raw canonical XDR extracted from that batch.
- `aws-public-ledger-64558277.metadata.xdr.base64`: same XDR as base64 text, the verifier input representation.
- `aws-public-pubnet-config.json`: public data-lake configuration.
- `onfinality-getledgers-64558277.json`: preserved original OnFinality JSON response wrapper.
- `onfinality-ledger-64558277.metadata.xdr`: raw XDR extracted from OnFinality's wrapper.
- `ledger-64558277-metadata.xdr.base64`: original OnFinality base64 payload used by the Stage 1–3 proof.
- `stellar-etl-62e06b1-ledger-64558277-reproduction.json`: original Stellar ETL transformer output bundle for the real target and control.

Verify the checked-in bytes with `sha256sum -c docs/evidence/SHA256SUMS` from the repository root.
