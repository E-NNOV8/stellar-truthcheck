# Flagship evidence and reproduction

## Classification

**SOURCE-REPRODUCED + INDEPENDENT METADATA-SOURCE CORROBORATION.** The target and control operation records are actual outputs of the original Stellar ETL transformer at commit `62e06b18892c074b624f446c908169412970d75f`, run on authentic Mainnet ledger-close metadata. The exact production Hubble export rows were not obtained. This is not **PRODUCTION-EXPORT VERIFIED**.

The verifier uses the official Stellar Go SDK v0.8.0. The canonical example is Mainnet ledger `64558277`, passphrase `Public Global Stellar Network ; September 2015`, protocol 28, ledger hash `b276434d7e1654f2b32a09d43891c76122f6865b136ab98a030bc36ccf50e49b`.

## Cases

### Failed target — FAIL / exit 2

Transaction `575f16436ec1d6eabfc8f66e2dec553e61b53ed85914b50736c842a0d567266b` has canonical result `TransactionResultCodeTxFailed` (`resource_limit_exceeded`). Its operation metadata has no committed operation effects for the attempted invoke-host-function operation. The original pinned ETL transform output contains nine `asset_balance_changes` transfer records. The ledger metadata also has diagnostic events; an event flag such as `in_successful_contract_call=true` does not prove enclosing transaction success or committed transfer. Fee processing is separate: 174334 stroops charged, 366624 collected, and 192290 refunded.

### Successful same-ledger control — PASS / exit 0

Transaction `afbf759fa9317cc7a8e28812c8b5b0245e9d31e55e9e9d8cb6a72e5eb832d57d` succeeded as a fee-bump inner transaction. Canonical committed operation metadata and the original transform output agree on a `15.1300000 USDC` transfer (`151300000` stroops), asset issuer `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`, from `GCNOWLX3ICSI5JVM4M443Y2WGOTJROVQLRWOEXGOJ6GM6J4CLNJ4IEQA` to `CCDCTXGN7FPCGSPV4PEVNZODDNHUMDQ3SAKFHYW6TCCDK3JXSX4UVS3Y`. Fee charged was 13163 stroops.

### Missing canonical input — UNKNOWN / exit 3

The reproducibility script passes a path that it first confirms does not exist. It checks for exit 3, status UNKNOWN, and the explicit canonical-evidence read diagnostic. Missing evidence is not treated as PASS.

## Reproduce

From the repository root with Go 1.26.10 and Python 3:

```sh
go mod download
go mod verify
go build -trimpath -buildvcs=false -o ./dist/truthcheck .
sha256sum -c docs/evidence/SHA256SUMS
python3 scripts/verify_flagship.py ./dist/truthcheck
```

The script validates process exit codes and structured fields, including the target hash/status/failure reason/nine records, control hash/status and exact committed movement match, and missing-evidence UNKNOWN diagnostic. GitHub Actions runs the same checks. No live service, BigQuery credentials, Hubble access, or cloud account is used by this proof.

## Source provenance and representation hashes

The AWS source object is the documented public Open Data object:

`https://aws-public-blockchain.s3.amazonaws.com/v1.1/stellar/ledgers/pubnet/FC279FFF--64512000-64575999/FC26EB3A--64558277.xdr.zst`

Its compressed bytes are preserved as `aws-public-ledger-64558277.xdr.zst` (SHA-256 `ec9d2e73dc9754e1d65492f55e6563ff52181f0fcf83e0ed746f8d507a031d82`). The extracted raw XDR bytes are `aws-public-ledger-64558277.metadata.xdr` (2,177,408 bytes; SHA-256 `a971741eb8d18882a34ee3e52d12b9807e5ef6bc26ea677ce4a220437034e659`). The corresponding base64 text consumed by the CLI is `aws-public-ledger-64558277.metadata.xdr.base64` (SHA-256 `c7605227bfb67c4c629f527a86eb954c412c065f79f6834dc1829fcb74bce3b2`). The compressed-object, decoded-XDR, and base64-text hashes refer to different representations and are not interchangeable.

The original OnFinality `getLedgers` JSON wrapper is preserved as `onfinality-getledgers-64558277.json` (SHA-256 `6774077099ea9450ca06b8ea1fcaf81fd32a3c882ea3110151d6ec6785516bb7`). Its extracted raw XDR bytes, `onfinality-ledger-64558277.metadata.xdr`, are SHA-256 `a971741eb8d18882a34ee3e52d12b9807e5ef6bc26ea677ce4a220437034e659`, byte-identical to the AWS extracted XDR. The original OnFinality base64 source file `ledger-64558277-metadata.xdr.base64` is SHA-256 `729b3ce5cfdb56d5e460a4eeddf2fc6d5f705b4d55957115329f11d5f948824e`; its decoded XDR bytes have the same raw XDR SHA above. Equality applies to the extracted LedgerCloseMeta payload, not to the AWS compressed object and OnFinality JSON wrapper.

The original transformer output bundle `stellar-etl-62e06b1-ledger-64558277-reproduction.json` is SHA-256 `ae9a13a4f319b5e640f688195b5cb35424cfa2e7e5dcd97ffe421c110cbcd9db`. It is source-reproduced output, not a production Hubble row.

Verify committed evidence hashes with:

```sh
sha256sum -c docs/evidence/SHA256SUMS
```

## What this does not prove

The official [Galexie data lake format documentation](https://developers.stellar.org/docs/data/indexers/build-your-own/galexie/data-lake-format) states that the format does not prove stored metadata is the metadata the network closed. Exact agreement between OnFinality and AWS establishes distribution-source byte agreement for this artifact; the sources may share upstream data. Truthcheck does not authenticate apply metadata against an SCP quorum or re-execute Core. The input must be trusted and complete. This evidence does not establish Hubble production behavior or general ledger-to-export correctness.
