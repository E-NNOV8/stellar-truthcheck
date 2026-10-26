# Stellar Truthcheck

Truthcheck independently verifies supported Stellar ETL-reported SAC transfers against committed execution evidence from trusted ledger-close metadata.

It is a small, offline Go CLI for developers and data engineers who need to check a supported Stellar ETL operation record against the corresponding ledger execution evidence. It does not retrieve data or authenticate the source of that evidence.

## Supported workflow

For one invoke-host-function operation record, Truthcheck:

1. Reads base64 `LedgerCloseMeta` XDR and a Stellar ETL operation-output JSON bundle.
2. Checks the selected ledger and transaction identity using the supplied network passphrase, header hash, transaction-set commitment, and transaction-result-set commitment.
3. Reads transaction success from the canonical transaction result and supported SAC `transfer` events from committed operation metadata (`TransactionMeta` v4).
4. Compares exact asset identity, source, destination, amount, direction, and multiplicity with ETL `asset_balance_changes`.

Current support is limited to `LedgerCloseMeta` v1/v2, `TransactionMeta` v4, and the implemented invoke-host-function/SAC transfer schema. See [known limitations](docs/KNOWN_LIMITATIONS.md) and [architecture](docs/architecture.md).

## Build and run

Requires Go 1.26.10. From the repository root:

```sh
go mod download
go mod verify
go build -o ./truthcheck .
go install -buildvcs=false .
```

Example using the preserved Mainnet failure case:

```sh
./truthcheck \
  --ledger-meta docs/evidence/ledger-64558277-metadata.xdr.base64 \
  --etl-output docs/evidence/stellar-etl-62e06b1-ledger-64558277-reproduction.json \
  --network-passphrase 'Public Global Stellar Network ; September 2015' \
  --transaction-hash 575f16436ec1d6eabfc8f66e2dec553e61b53ed85914b50736c842a0d567266b \
  --etl-source-revision 62e06b18892c074b624f446c908169412970d75f
```

This example returns **FAIL** and exits `2`: the canonical transaction failed, while the preserved output from the pinned ETL transformer contains nine reported transfer movements. It is source-reproduced evidence, not a retrieved Hubble production row.

## Verdicts and exit codes

- **PASS / 0** — supported ETL movement records match the supported committed canonical movement evidence.
- **FAIL / 2** — a supported exported movement claim conflicts with canonical execution evidence.
- **UNKNOWN / 3** — required evidence is missing, malformed, ambiguous, or unsupported. UNKNOWN is not success.

The Stage 4 proof bundle also contains a real successful control (`15.1300000 USDC`) and provenance for the AWS and OnFinality metadata inputs. See [reproduction steps and evidence](docs/EVIDENCE.md).

## Trust boundary

The caller must provide complete ledger-close metadata from a source they trust. The ledger header commits the transaction set and transaction results, but does not directly authenticate apply-metadata event bytes. Matching archive copies do not prove consensus authenticity. Truthcheck does not re-execute Stellar Core.

The historical example is **SOURCE-REPRODUCED with INDEPENDENT METADATA-SOURCE CORROBORATION**. The exact historical Hubble production export remains unavailable; this project does not claim production Hubble verification.

## Contributing and license

See [CONTRIBUTING.md](CONTRIBUTING.md) for build and validation commands. Truthcheck is licensed under Apache-2.0; see [LICENSE](LICENSE). The Go module path is `github.com/E-NNOV8/stellar-truthcheck`.
