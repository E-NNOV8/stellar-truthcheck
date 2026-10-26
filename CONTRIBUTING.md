# Contributing

Truthcheck is intentionally narrow. Changes should preserve the distinction between transaction-level success, committed operation effects, diagnostic events, and fees. Do not turn missing or unsupported evidence into PASS.

## Requirements

- Go 1.26.10
- Network access to the Go module proxy on first dependency download

The supported SDK is pinned in `go.mod`/`go.sum` (`github.com/stellar/go-stellar-sdk v0.8.0`).

## Local checks

From the repository root:

```sh
go mod download
go mod verify
test -z "$(gofmt -l main.go main_test.go)"
go vet ./...
go test -count=1 ./...
go build -trimpath -buildvcs=false -o ./dist/truthcheck .
sha256sum -c docs/evidence/SHA256SUMS
python3 scripts/verify_flagship.py ./dist/truthcheck
```

The flagship script checks the actual CLI process exit codes and parses its JSON output for the failed target, successful control, and missing-evidence case. It does not treat any negative CLI exit as success without checking the result contents and identity.

## Scope for changes

- Preserve exact integer/stroop comparisons.
- Keep ETL output as the claim being checked and canonical XDR as the evidence source.
- Add tests for any newly supported event or metadata shape, including malformed and incomplete evidence.
- Do not describe the preserved source reproduction as a production Hubble export.
- Do not add credentials or network calls to the offline verification path.

Truthcheck is licensed under Apache-2.0; see LICENSE.
