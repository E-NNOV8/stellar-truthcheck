# Check your own supported ETL operation

## Who this is for

A Stellar data engineer or indexer maintainer who already has a Stellar ETL
invoke-host-function operation output and wants to check its SAC transfer claims.
You need one original operation object, its transaction hash, zero-based operation
index, network passphrase, and complete metadata for that ledger. No wallet,
signing key, Hubble subscription, or cloud credentials are required by Truthcheck.
An ETL operator must supply their output; arbitrary Hubble/BigQuery table rows,
Horizon operation/effect responses, and other indexer schemas are not supported.

This workflow checks **movement claims**, not the exporter's transaction-success
or fee columns. Preparation reads success, fee, and processing index from supplied
canonical metadata as comparison context. It does not independently verify every
field in the ETL object, nor authenticate the metadata provider.

## 1. Build

Install Go 1.26.10; Python 3 and curl are used only by the acquisition/examples
below. From a clean source checkout:

```sh
export GOTOOLCHAIN=go1.26.10
go mod download
go mod verify
go build -trimpath -buildvcs=false -o ./dist/truthcheck .
./dist/truthcheck --help
./dist/truthcheck prepare --help
```

Go may download the specified toolchain. Build success is silent; help exits 0
and prints flags. All verification and preparation after download are offline.

## 2. Obtain complete ledger metadata

Choose a source you trust. A transaction's result/meta XDR alone, ledger header,
or classic history archive transaction file is **not** a complete LedgerCloseMeta.
Truthcheck supports these representations with explicit `--ledger-format`:

| Format | Input | Selection |
| --- | --- | --- |
| `base64` (default) | One base64 LedgerCloseMeta text file | Optional sequence cross-check |
| `xdr` | One raw binary LedgerCloseMeta | Optional sequence cross-check |
| `rpc` | Full JSON-RPC getLedgers response with base64 metadataXdr | Required `--ledger-sequence` |
| `galexie` | Zstd-compressed LedgerCloseMetaBatch (.xdr.zst) | Required `--ledger-sequence` |

### Public historical metadata: AWS Open Data

The [official format guide](https://developers.stellar.org/docs/data/indexers/build-your-own/galexie/data-lake-format)
explains public object names and `.config.json`. No AWS account is needed for
these HTTP GETs. The public source can be unavailable or lack your ledger; an
HTTP failure is an acquisition failure, not a Truthcheck verdict.

For a chosen Mainnet ledger, generate the URL from the downloaded configuration
(do not assume every data lake has these batch/partition sizes):

```sh
mkdir -p inputs
curl --fail --location --max-time 60 \
  https://aws-public-blockchain.s3.amazonaws.com/v1.1/stellar/ledgers/pubnet/.config.json \
  -o inputs/config.json
export LEDGER=64558278
python3 - <<'PY' > inputs/ledger-url.txt
import json, os
c = json.load(open('inputs/config.json'))
assert c['compression'] == 'zstd'
n = int(os.environ['LEDGER'])
b = c['ledgersPerBatch']; p = b * c['batchesPerPartition']
start = n // b * b; partition = n // p * p
batch = f'{0xffffffff-start:08X}--{start}'
if b > 1:
    batch += f'-{start+b-1}'
prefix = '' if c['batchesPerPartition'] == 1 else f'{0xffffffff-partition:08X}--{partition}-{partition+p-1}/'
print('https://aws-public-blockchain.s3.amazonaws.com/v1.1/stellar/ledgers/pubnet/' + prefix + batch + '.xdr.zst')
PY
curl --fail --location --max-time 60 "$(cat inputs/ledger-url.txt)" -o inputs/ledger.xdr.zst
sha256sum inputs/config.json inputs/ledger.xdr.zst
```

Use `--ledger-format galexie --ledger-sequence "$LEDGER"`. No decompression,
batch extraction, or base64 conversion is needed. The adapter limits zstd decoder
memory to 256 MiB; larger batches require a separately extracted LedgerCloseMeta.

### RPC metadata

Alternatively, use a provider or your own RPC node that retains the ledger. RPC
access, credentials, limits, and history retention depend on the provider;
Mainnet access is not promised to be public/free. The official
[getLedgers reference](https://developers.stellar.org/docs/data/apis/rpc/api-reference/methods/getLedgers)
describes `metadataXdr` and retention. With your endpoint in `RPC_URL` and the
chosen sequence in `LEDGER`:

```sh
python3 - <<'PY' > inputs/request.json
import json, os
print(json.dumps({'jsonrpc':'2.0','id':1,'method':'getLedgers',
                  'params':{'startLedger':int(os.environ['LEDGER']),
                            'pagination':{'limit':1},'xdrFormat':'base64'}}))
PY
curl --fail --max-time 60 -H 'Content-Type: application/json' \
  --data-binary @inputs/request.json "$RPC_URL" -o inputs/getledgers.json
```

Use `--ledger-meta inputs/getledgers.json --ledger-format rpc --ledger-sequence
"$LEDGER"` in both commands below. RPC error responses, absent/duplicate ledger
matches, missing metadata, and differing wrapper hashes are rejected. A successful
HTTP response does not mean RPC succeeded. Keep endpoint credentials out of
committed provenance files. Live RPC acquisition was not exercised in this task;
the preserved real OnFinality response was exercised through the adapter.

## 3. Obtain one supported ETL operation object

From your existing ETL pipeline, preserve one original operation object as
`inputs/operation.json`. This is the JSON emitted by
[`TransformOperation` at the demonstrated revision](https://github.com/stellar/stellar-etl/blob/62e06b18892c074b624f446c908169412970d75f/internal/transform/operation.go),
not a Truthcheck target/control wrapper. Other revisions may be compatible but
must supply the implemented shape. The required subset is:

- `ledger_sequence`, `type: 24`, `type_string: "invoke_host_function"`;
- integer `transaction_id` and `id`, the original Stellar ETL total-order IDs;
- `details.asset_balance_changes`: a non-null array of supported transfer objects
  (`type`, `asset_type`, `asset_code`/`asset_issuer` for credit assets, `from`, `to`,
  and decimal-string `amount`). An empty array is valid only when actually exported.

Preserve all fields and exact integer/amount tokens. In particular, do not pass
64-bit IDs through JavaScript floating point, round amounts, merge movements,
replace null with `[]`, or copy canonical transfers into the claim file.
`details_json` is not a substitute for `details`.

If your export is JSONL, extract the row with a parser that preserves integers
(e.g. Python's `json`), selecting the ETL `id` from your pipeline. Do not select
solely by amount/address. For a JSONL file and `OPERATION_ID` in the environment:

```sh
python3 - <<'PY' > inputs/operation.json
import json, os
wanted = int(os.environ['OPERATION_ID'])
with open('operations.jsonl') as f:
    rows = [row for line in f if (row := json.loads(line)).get('id') == wanted]
assert len(rows) == 1, f'expected one operation, found {len(rows)}'
print(json.dumps(rows[0]))
PY
```

Get the transaction hash and operation index from your export job's transaction
context. For fee-bump transactions use the **inner** hash stored in the ledger's
result pair, not the outer envelope hash. Processing index is one-based;
operation index supplied to Truthcheck is **zero-based**, so first operation is 0.
If you only have a transaction ID, inspect your pipeline's transaction output or
SDK ingestion context. Preparation checks the row's IDs against the selected
canonical result position and operation index; it refuses a different row.

Users without ETL access can run the original pinned transformer locally on
public metadata (see the executed example below). That is source reproduction,
not retrieval of their production export. It cannot establish production behavior.

## 4. Prepare, then verify

Set these to your actual transaction, operation, and source revision:

```sh
export TX_HASH=YOUR_64_CHARACTER_TRANSACTION_HASH
export OP_INDEX=0
export ETL_REVISION=YOUR_EXPORTER_SOURCE_REVISION
export NETWORK_PASSPHRASE='Public Global Stellar Network ; September 2015'
./dist/truthcheck prepare \
  --ledger-meta inputs/ledger.xdr.zst --ledger-format galexie --ledger-sequence "$LEDGER" \
  --operation-output inputs/operation.json \
  --transaction-hash "$TX_HASH" --operation-index "$OP_INDEX" \
  --network-passphrase "$NETWORK_PASSPHRASE" > inputs/bundle.json
```

**Check exit status before continuing.** Preparation exits 0 with a bundle; that
is not PASS. Failure emits UNKNOWN JSON and exits 3; do not use it as a bundle.
Use a fresh output path to preserve any earlier work. Preparation preserves the
original operation object and derives only wrapper identity/context from the
supplied ledger. It can prepare a supported disagreement; it never repairs claims.
Keep the original row, original metadata, their hashes, exporter revision, and
acquisition source alongside the prepared bundle.

```sh
./dist/truthcheck \
  --ledger-meta inputs/ledger.xdr.zst --ledger-format galexie --ledger-sequence "$LEDGER" \
  --etl-output inputs/bundle.json --transaction-hash "$TX_HASH" \
  --network-passphrase "$NETWORK_PASSPHRASE" \
  --etl-source-revision "$ETL_REVISION" > inputs/report.json
code=$?
cat inputs/report.json
printf 'Truthcheck exit: %s\n' "$code"
```

Do not use `go run` to interpret verdict exit codes: it can wrap nonzero program
exits. In shell scripts using `set -e`, capture expected FAIL/UNKNOWN explicitly,
for example `code=0; ./dist/truthcheck ... > report.json || code=$?`.

| Result | Meaning | Next action |
| --- | --- | --- |
| PASS / 0 | Exact supported movement multiset agrees, including multiplicity | Retain report and inputs; no claim about all ledger effects or pipeline health |
| FAIL / 2 | Supported movement claim contradicts execution or committed transfers | Inspect both movement lists and transaction result; trace exporter transform; do not treat diagnostic transfers as settled |
| UNKNOWN / 3, `decode`, missing fields, ID mismatch | Invalid/incomplete evidence or wrong selection | Fix acquisition/row selection; preserve exact original claims; rerun |
| UNKNOWN / 3, `unsupported`, unverified SAC, amount range | Evidence outside implemented semantics | Obtain a supported case or add separately reviewed support; do not count as agreement |
| UNKNOWN / 3, read error or RPC error | File/access/provider/retention problem | Correct path/access or use trusted historical source; rerun |
| 1 | Output write failure | Check destination space/permissions; no usable verdict |

The report's `reason` gives the specific cause. All input/unsupported/infrastructure
categories remain UNKNOWN; they are not newly distinct machine status codes.
Header/transaction-set/result-set commitments and network-derived transaction
hashes are checked. Header commitments do **not** authenticate apply-metadata
event bytes. The ledger source must be trusted and complete; Truthcheck does not
verify SCP consensus or re-execute Core. Exact comparison uses integer stroops,
never floats. See [limitations](KNOWN_LIMITATIONS.md) for event/version/asset scope.

## 5. Additional real case and reproducible source transformation

The new demonstration uses Mainnet ledger **64558278**, independently downloaded
from the public AWS URL generated above. It is a different ledger and transaction
from both historical cases. It is not synthetic and not a production export.
The source runner below calls the pinned original ETL transformer, selecting the
specified invoke operation with nine exported transfers. It never
constructs transfer claims with Truthcheck's canonical parser.

With the downloaded object at `inputs/ledger.xdr.zst`, put an absolute path in
`DEMO_INPUT` and the desired original row output path in `DEMO_OUTPUT`, then run:

```sh
export DEMO_INPUT="$PWD/inputs/ledger.xdr.zst"
export DEMO_OUTPUT="$PWD/inputs/operation.json"
export ETL_REVISION=62e06b18892c074b624f446c908169412970d75f
export ETL_SCRATCH="$(mktemp -d)"
curl --fail --location --max-time 120 \
  "https://api.github.com/repos/stellar/stellar-etl/tarball/$ETL_REVISION" \
  -o "$ETL_SCRATCH/source.tar.gz"
tar -xzf "$ETL_SCRATCH/source.tar.gz" -C "$ETL_SCRATCH" --strip-components=1
cat > "$ETL_SCRATCH/truthcheck_demo.go" <<'GO'
package main
import (
 "encoding/json"
 "fmt"
 "os"
 "github.com/klauspost/compress/zstd"
 "github.com/stellar/go-stellar-sdk/xdr"
 "github.com/stellar/stellar-etl/v2/internal/input"
 "github.com/stellar/stellar-etl/v2/internal/transform"
)
func main() {
 raw,e:=os.ReadFile(os.Args[1]); must(e)
 z,e:=zstd.NewReader(nil); must(e); defer z.Close()
 decoded,e:=z.DecodeAll(raw,nil); must(e)
 var batch xdr.LedgerCloseMetaBatch; must(xdr.SafeUnmarshal(decoded,&batch))
 lcm,e:=batch.GetLedger(64558278); must(e)
 pass:="Public Global Stellar Network ; September 2015"
 ops,e:=input.OperationsFromLedger(lcm,pass); must(e)
 for _,op:=range ops {
  if op.Operation.Body.Type!=xdr.OperationTypeInvokeHostFunction {continue}
  out,e:=transform.TransformOperation(op.Operation,op.OperationIndex,op.Transaction,op.LedgerSeqNum,lcm,pass)
  if e!=nil {fmt.Fprintln(os.Stderr,e); continue}
  changes,ok:=out.OperationDetails["asset_balance_changes"].([]map[string]interface{})
  if !ok || len(changes)==0 {continue}
  if op.Transaction.Result.TransactionHash.HexString()!="5a177f7748ba33ba8bfd62221e1a585300ffa387339d1a3f675dd089141baa75" {continue}
  b,e:=json.MarshalIndent(out,"","  "); must(e); must(os.WriteFile(os.Args[2],b,0644))
  fmt.Printf("hash=%s index=%d operation=%d changes=%v\n",op.Transaction.Result.TransactionHash.HexString(),op.Transaction.Index,op.OperationIndex,changes)
  return
 }
 panic("selected transfer operation not found")
}
func must(e error) {if e!=nil {panic(e)}}
GO
(cd "$ETL_SCRATCH" && GOTOOLCHAIN=go1.26.10 go run ./truthcheck_demo.go "$DEMO_INPUT" "$DEMO_OUTPUT")
```

This optional source reproduction downloads the ETL project's larger dependency
set and executes its source. It requires Go, network access, and disk space; it is
separate from Truthcheck's offline verifier and is not required for existing ETL
operators. The runner is specific to ledger 64558278, not a general export CLI.

Executed result (2026-11-14): transaction
`5a177f7748ba33ba8bfd62221e1a585300ffa387339d1a3f675dd089141baa75`, processing
index 289, operation 0, ledger hash
`3444bd0bb1771c6a7b848399e950f5392c075ea495ff1162a0a1ab6bd604d9b6`.
The canonical result is `TransactionResultCodeTxFailed` with no committed
transfers; the upstream transform reports nine transfers. Expected **FAIL / 2**;
preparation actually exited **0**, verification actually returned **FAIL / 2**:

```json
{"status":"FAIL","reason":"ETL claims 9 committed asset transfer(s) for a transaction that failed; diagnostic contract events do not establish committed movement"}
```

To finish the example, set these values and execute section 4's preparation and
verification commands with the files downloaded/generated above:

```sh
export LEDGER=64558278
export TX_HASH=5a177f7748ba33ba8bfd62221e1a585300ffa387339d1a3f675dd089141baa75
export OP_INDEX=0
export NETWORK_PASSPHRASE='Public Global Stellar Network ; September 2015'
export ETL_REVISION=62e06b18892c074b624f446c908169412970d75f
```

Recorded SHA-256 hashes (different representations must be compared separately):

| Artifact | SHA-256 |
| --- | --- |
| AWS compressed batch | `141eff1d35ca1da2a202b0bd575ae81dfd6a17f38bbdcdaa1297abc4840d64ec` |
| Original transformed operation JSON | `a8828c5f81ebc20ff379ba56fc44b67931e3cc0b61aa5bfa153e8ab5c9e8934e` |
| Prepared bundle JSON | `ad9ead7ad95e0062c4478e589068c61c889b15737f1896a3144123b693cfd252` |

This is another real failed-transaction comparison, not another successful
transfer case. Source-reproduced ETL and canonical evidence share the downloaded
metadata; this tests independent interpretation, not production export acquisition
or provider authenticity. The original preserved same-ledger successful control
continues to test PASS. No new production Hubble row was obtained.

## Verification and readiness

Executed on 2026-11-14 with Go 1.26.10:

- `go mod download`, `go mod verify`, Go formatting, `go vet ./...`,
  `go test -count=1 ./...`, `go test -count=1 -race ./...`, and CLI build passed.
- All eight preserved evidence checksums and `scripts/verify_flagship.py` passed:
  original FAIL/2, successful exact-transfer PASS/0, missing-input UNKNOWN/3.
- An isolated source copy (base archive plus this change, separate empty Go build
  cache, shared verified module/toolchain cache) executed the guide's build/help,
  public configuration download, URL generation, ledger download, pinned upstream
  source reproduction, preparation, and verification commands. It reproduced the
  new FAIL/2 and both recorded operation/bundle hashes. No network was needed for
  the Truthcheck preparation or verification steps themselves.
- Adapters exercised preserved real base64, raw XDR, RPC JSON, and Galexie inputs;
  tests rejected incorrect IDs/sequences, rounded integer IDs, duplicate keys,
  ambiguous RPC matches, RPC errors, missing/null claims, and unsupported input.
- A deliberately changed amount remained changed after preparation and returned
  FAIL; that mutation is a synthetic regression test, not the real demonstration.
- README build/install and its historical example were also exercised in the
  isolated source copy. The original plain build hit a VCS-stamping error in the
  source-only environment; README now uses `-buildvcs=false`, as the contribution
  checks already did. The corrected build/install passed and the example exited 2.
- Live RPC acquisition and production ETL/Hubble access were not executed.

The identified usability gap is demonstrated by a raw original ETL object that
previously returned `matching ETL operation record is missing`, and `--help` that
previously returned UNKNOWN without usage. Preparation and storage adapters now
remove those manual wrapper/conversion steps. No canonical movement parsing,
commitment checking, exact comparison, or verdict rules were relaxed.

| Criterion | Assessment |
| --- | --- |
| User value | An ETL operator can inspect a concrete transfer disagreement; recurring operator demand remains unvalidated |
| Stellar centrality | Directly uses Stellar ledger metadata, network hashes, transaction results, SAC identity, and operation events |
| Workflow completeness | Complete for the documented operator/raw-operation shape and supported metadata; obtaining a production export still requires operator access |
| Correctness | Existing semantic checks preserved; adapter/identity/preservation regression coverage added |
| Appropriate proof | Historical positive/negative evidence plus a new real-ledger source reproduction; no production Hubble claim |
| Ease of use | Offline preparation, storage-format adapters, usable help, exact commands, and actionable diagnostic guidance; still an expert CLI |
| Distinct contribution | Independent committed-evidence interpretation checks ETL diagnostic-event claims; broader differentiation/adoption is not established |
| Honest scope | Narrow SAC transfer/version/schema support; trusted metadata required; no general reconciliation or consensus-authenticity claim |

The Drips rejection has no supplied explanation. These fixes address demonstrated
workflow gaps, not a confirmed rejection reason. They do not guarantee acceptance.
