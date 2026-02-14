# Nimbus

Nimbus is an extension to the Algorand node that adds programmable hooks and a cryptographic receipt chain. Hooks are AVM programs that run against every block, producing derived state with tamper-evident receipts. Think of them as read-only smart contracts that observe the blockchain without participating in consensus.

## Table of Contents

- [Concepts](#concepts)
- [Quick Start with Docker](#quick-start-with-docker)
- [Manual Node Setup](#manual-node-setup)
- [Writing Hook Programs](#writing-hook-programs)
- [API Reference](#api-reference)
- [Receipt Chain and Verification](#receipt-chain-and-verification)
- [Configuration Reference](#configuration-reference)
- [Examples](#examples)
- [Troubleshooting](#troubleshooting)

---

## Concepts

### What is a Hook?

A hook is a compiled AVM (TEAL) program registered with a Nimbus node. Every time the node receives a new block, it evaluates each hook by simulating an application call transaction. The hook receives the previous round's state as input and produces new state as output (via its last log message).

### What is the Receipt Chain?

Each hook evaluation produces a cryptographic receipt that binds the hook's output state to the block it was evaluated against. Receipts are chained together: each receipt references the hash of the previous receipt, forming a tamper-evident linked list. Anyone with the hook program and its history can independently verify the chain was not altered.

### Architecture

A Nimbus node extends the follower node. It does not participate in consensus. It follows a relay node and evaluates hooks against each new block.

```
Relay Node (consensus)  --->  Nimbus Node (hooks + receipts)
     port 8080                      port 8081
```

---

## Quick Start with Docker

The fastest way to run a Nimbus node is with Docker Compose. This starts a private network with a relay node and a Nimbus node in a single container.

### Prerequisites

- Docker and Docker Compose installed
- The go-nimbus source tree checked out

### Start the Sandbox

```bash
docker compose -f docker-compose.nimbus.yml up -d
```

This creates a `nimbus_sandbox` project with:

| Service     | Host Port | Description       |
|-------------|-----------|-------------------|
| Nimbus API  | 4101      | Hook management   |
| KMD         | 4102      | Key management    |
| Relay API   | 4103      | Consensus node    |

All endpoints use the dev token:
```
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
```

### Verify It Works

```bash
# Check node status
curl -s \
  -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  http://localhost:4101/v2/status

# List hooks (empty on first start)
curl -s \
  -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  http://localhost:4101/v2/nimbus/hooks
```

### Stop and Clean Up

```bash
# Stop the sandbox (preserves data)
docker compose -f docker-compose.nimbus.yml down

# Stop and delete all data
docker compose -f docker-compose.nimbus.yml down -v
```

### Rebuild After Code Changes

```bash
docker compose -f docker-compose.nimbus.yml up -d --build
```

---

## Manual Node Setup

If you prefer to run Nimbus outside Docker, you need two nodes: a relay for consensus and a Nimbus follower.

### 1. Build from Source

```bash
make build
```

### 2. Create a Private Network

Create a network template file `nimbus_net.json`:

```json
{
  "Genesis": {
    "ConsensusProtocol": "future",
    "NetworkName": "nimbusnet",
    "FirstPartKeyRound": 0,
    "LastPartKeyRound": 30000,
    "Wallets": [
      { "Name": "Wallet1", "Stake": 40, "Online": true },
      { "Name": "Wallet2", "Stake": 40, "Online": true },
      { "Name": "Wallet3", "Stake": 20, "Online": true }
    ],
    "DevMode": true
  },
  "Nodes": [
    {
      "Name": "data",
      "IsRelay": true,
      "Wallets": [
        { "Name": "Wallet1", "ParticipationOnly": false },
        { "Name": "Wallet2", "ParticipationOnly": false },
        { "Name": "Wallet3", "ParticipationOnly": false }
      ]
    },
    {
      "Name": "nimbus",
      "IsRelay": false,
      "ConfigJSONOverride": "{\"EnableNimbusMode\":true,\"EnableFollowMode\":true,\"Archival\":true,\"EnableDeveloperAPI\":true,\"EnableTxnEvalTracer\":true,\"EndpointAddress\":\"0.0.0.0:8081\"}"
    }
  ]
}
```

### 3. Create and Start the Network

```bash
goal network create -n mynet -r ~/nimbus-net -t nimbus_net.json
goal network start -r ~/nimbus-net
```

### 4. Set API Tokens

```bash
echo "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" > ~/nimbus-net/nimbus/algod.token
echo "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" > ~/nimbus-net/nimbus/algod.admin.token
```

### 5. Verify

```bash
curl -s \
  -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  http://localhost:8081/v2/nimbus/hooks
```

---

## Writing Hook Programs

Hooks are AVM programs. They are simulated as application call transactions against the ledger state at each block round. The key differences from a normal smart contract:

1. **Input**: The previous round's state is passed as `ApplicationArgs[0]` (accessible via `txna ApplicationArgs 0` in TEAL or `Txn.applicationArgs(0)` in puya-ts).
2. **Output**: The last log message emitted by the program becomes the new state. Use `log` in TEAL or the ARC4 return pattern in puya-ts.
3. **No side effects**: Hooks run in simulation mode. They cannot modify on-chain state, create inner transactions, or spend funds.
4. **Execution context**: The sender is the block's fee sink address. The transaction's FirstValid is the current block round.

### Removed AVM Constraints

When running in Nimbus mode, several AVM constraints that normally apply to on-chain smart contracts are removed. This allows hooks to perform arbitrarily complex computations without hitting the limits designed for consensus-critical execution.

| Constraint | Normal AVM Behavior | Nimbus Mode |
|---|---|---|
| **Opcode budget** | 700 per app call, pooled across group | Unlimited (`math.MaxInt`) |
| **Log size** | 1024 bytes per log call | Unlimited |
| **Log call count** | 32 log calls per execution | Unlimited |
| **Application args size** | 2048 bytes total across all args | No limit enforced |
| **Application args count** | 16 args maximum | No limit enforced |
| **App call depth** | 8 levels of inner app calls | No limit enforced |
| **Extra opcode budget** | Capped by `MaxExtraOpcodeBudget` | No limit enforced |
| **ClearState budget isolation** | Separate budget for clear state | Bypassed |

These removals mean your hook programs can:

- Process large amounts of data in a single execution
- Emit state of any size via `log`
- Call `log` as many times as needed (only the last log is used as state)
- Accept arbitrarily large input state via application args
- Perform deeply nested computations

The only hard constraint is that the program must emit at least one log message, or the evaluation records an error.

### TEAL Example: Counter Hook

This hook increments a counter each block:

```teal
#pragma version 11

// Load previous state from arg 0
txna ApplicationArgs 0
len
bz init

// Parse the counter from previous state (8-byte big-endian uint64)
txna ApplicationArgs 0
btoi
int 1
+
itob
log
int 1
return

init:
// First run: start at 1
int 1
itob
log
int 1
return
```

### Compiling TEAL to Bytecode

Use `goal clerk compile` to get the base64-encoded bytecode:

```bash
goal clerk compile counter.teal -o counter.tok
base64 < counter.tok
```

Or compile in Docker:

```bash
docker exec nimbus_sandbox_algod goal clerk compile /dev/stdin <<'EOF' -o /dev/stdout | base64
#pragma version 11
txna ApplicationArgs 0
len
bz init
txna ApplicationArgs 0
btoi
int 1
+
itob
log
int 1
return
init:
int 1
itob
log
int 1
return
EOF
```

### Algorand TypeScript (puya-ts) Hooks

For a higher-level development experience, use the `@akitafoundation/nimbus-hooks` package. Hooks extend `HookContract` instead of `Contract` or `LogicSig`. The only constraint on the `run` method is that the parameter type and return type must match:

```bash
npm install @akitafoundation/nimbus-hooks @algorandfoundation/algorand-typescript
```

```typescript
// counter.algo.ts
import { bytes, btoi, itob, Uint64 } from '@algorandfoundation/algorand-typescript'
import { HookContract } from '@akitafoundation/nimbus-hooks'

class BlockCounter extends HookContract {
  public run(previousState: bytes): bytes {
    if (previousState.length > 0) {
      const prev = btoi(previousState)
      return itob(prev + Uint64(1))
    }
    return itob(Uint64(1))
  }
}
```

Compile with the AlgoKit CLI:

```bash
algokit compile ts counter.algo.ts --out-dir out
```

See the [nimbus-hooks](https://github.com/kylebeee/nimbus-hooks) repository for the full SDK, TypeScript client, and additional examples.

---

## API Reference

All endpoints are served by the Nimbus node (port 8081 in Docker, mapped to 4101 on the host).

Read endpoints require the API token. Write endpoints (POST, DELETE) require the admin token.

### Set Up Shell Variables

```bash
export NIMBUS_URL="http://localhost:4101"
export NIMBUS_TOKEN="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
```

### List Hooks

```
GET /v2/nimbus/hooks
```

```bash
curl -s -H "X-Algo-API-Token: $NIMBUS_TOKEN" "$NIMBUS_URL/v2/nimbus/hooks"
```

Response:

```json
{
  "hooks": [
    {
      "id": "block-counter",
      "program-hash": "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=",
      "require-origin": false,
      "created-at": "2026-02-13T17:22:57.208223Z"
    }
  ]
}
```

### Create Hook

```
POST /v2/nimbus/hooks
```

Request body (JSON):

| Field            | Type    | Required | Description                                         |
|------------------|---------|----------|-----------------------------------------------------|
| `id`             | string  | yes      | Unique identifier for the hook                      |
| `program`        | string  | yes      | Base64-encoded compiled TEAL bytecode               |
| `require-origin` | boolean | no       | If true, backfills from genesis (requires archival)  |
| `initial-state`  | string  | no       | Base64-encoded seed state for the first evaluation   |

```bash
curl -s -X POST \
  -H "X-Algo-API-Token: $NIMBUS_TOKEN" \
  -d '{
    "id": "block-counter",
    "program": "CyABASYBBGluaXQxGBJEMRkUIkMxGRREIhNBAAoxGSMIFxRQsEAiQw==",
    "require-origin": false,
    "initial-state": ""
  }' \
  "$NIMBUS_URL/v2/nimbus/hooks"
```

Returns `204 No Content` on success.

### Get Hook

```
GET /v2/nimbus/hooks/:hookID
```

```bash
curl -s -H "X-Algo-API-Token: $NIMBUS_TOKEN" "$NIMBUS_URL/v2/nimbus/hooks/block-counter"
```

Response:

```json
{
  "id": "block-counter",
  "program-hash": "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=",
  "require-origin": false,
  "created-at": "2026-02-13T17:22:57.208223Z"
}
```

### Delete Hook

```
DELETE /v2/nimbus/hooks/:hookID
```

```bash
curl -s -X DELETE \
  -H "X-Algo-API-Token: $NIMBUS_TOKEN" \
  "$NIMBUS_URL/v2/nimbus/hooks/block-counter"
```

Returns `204 No Content` on success.

### Get Latest State

```
GET /v2/nimbus/hooks/:hookID/state
```

```bash
curl -s -H "X-Algo-API-Token: $NIMBUS_TOKEN" "$NIMBUS_URL/v2/nimbus/hooks/block-counter/state"
```

Response:

```json
{
  "round": 42,
  "state": "AAAAAAAAACU=",
  "error": "",
  "timestamp": "2026-02-13T17:23:05.123456Z",
  "block-hash": "blN1+1hy7RLkvY...",
  "program-hash": "47DEQpj8HBSa...",
  "state-hash": "W6nwgSHN8sR...",
  "prev-hash": "kX3b7nJK9Qm...",
  "receipt-hash": "rT4mFvB2x..."
}
```

The `state` field is base64-encoded. For the counter example, decode it to get an 8-byte big-endian uint64.

### Get State History

```
GET /v2/nimbus/hooks/:hookID/history?from=10&to=20
```

Both `from` and `to` are optional. Omit them to get the full history.

```bash
# Full history
curl -s -H "X-Algo-API-Token: $NIMBUS_TOKEN" \
  "$NIMBUS_URL/v2/nimbus/hooks/block-counter/history"

# Rounds 10 through 20
curl -s -H "X-Algo-API-Token: $NIMBUS_TOKEN" \
  "$NIMBUS_URL/v2/nimbus/hooks/block-counter/history?from=10&to=20"

# From round 100 onward
curl -s -H "X-Algo-API-Token: $NIMBUS_TOKEN" \
  "$NIMBUS_URL/v2/nimbus/hooks/block-counter/history?from=100"
```

Response:

```json
{
  "history": [
    {
      "round": 10,
      "state": "AAAAAAAAAAo=",
      "timestamp": "2026-02-13T17:23:00.000000Z",
      "block-hash": "...",
      "program-hash": "...",
      "state-hash": "...",
      "prev-hash": "...",
      "receipt-hash": "..."
    },
    {
      "round": 11,
      "state": "AAAAAAAAAAs=",
      "timestamp": "2026-02-13T17:23:01.000000Z",
      "block-hash": "...",
      "program-hash": "...",
      "state-hash": "...",
      "prev-hash": "...",
      "receipt-hash": "..."
    }
  ]
}
```

### Verify Receipt Chain

```
GET /v2/nimbus/hooks/:hookID/verify
```

Validates the entire receipt chain for a hook. This checks that every receipt correctly links to its predecessor, the program hash is consistent, and state hashes match.

```bash
curl -s -H "X-Algo-API-Token: $NIMBUS_TOKEN" \
  "$NIMBUS_URL/v2/nimbus/hooks/block-counter/verify"
```

Response (valid chain):

```json
{
  "valid": true,
  "entries": 100,
  "first_round": 0,
  "last_round": 99
}
```

Response (broken chain):

```json
{
  "valid": false,
  "entries": 100,
  "first_round": 0,
  "last_round": 99,
  "broken_at": 57,
  "error": "round 57: state hash mismatch"
}
```

---

## Receipt Chain and Verification

Each time a hook is evaluated against a block, a receipt is computed:

```
Receipt = Hash("NR" || Round || BlockHash || ProgramHash || StateHash || PrevHash)
```

Where:

| Field         | Size     | Description                                      |
|---------------|----------|--------------------------------------------------|
| `Round`       | 8 bytes  | Block round number (big-endian uint64)            |
| `BlockHash`   | 32 bytes | SHA-512/256 hash of the block header              |
| `ProgramHash` | 32 bytes | SHA-512/256 hash of the hook program bytecode     |
| `StateHash`   | 32 bytes | SHA-512/256 hash of the hook's output state       |
| `PrevHash`    | 32 bytes | The previous round's ReceiptHash (zero at genesis)|

The `"NR"` prefix is the domain separator (stands for "NimbusReceipt"), ensuring these hashes are prefix-free from all other Algorand hash domains.

### Genesis Receipt

At round 0, the hook has not been evaluated against a real block. The genesis receipt uses:

- `BlockHash` = zero digest (no block)
- `PrevHash` = zero digest (no predecessor)
- `StateHash` = hash of the initial state (or zero bytes if none)

### Chain Properties

- **Tamper-evident**: Changing any state entry invalidates all subsequent receipts.
- **Block-anchored**: Each receipt includes the block header hash, binding the derived state to the canonical chain.
- **Program-locked**: The program hash in every receipt proves the same program was used throughout.
- **Independently verifiable**: Anyone with the hook program and history can recompute and verify every receipt.

---

## Configuration Reference

These config fields control Nimbus behavior. Set them in `config.json` or via the network template's `ConfigJSONOverride`.

| Field                   | Type | Default | Description                                          |
|-------------------------|------|---------|------------------------------------------------------|
| `EnableNimbusMode`      | bool | false   | Enables hook management and evaluation               |
| `EnableFollowMode`      | bool | false   | Required. Nimbus extends follower mode               |
| `Archival`              | bool | false   | Required for hooks with `require-origin: true`       |
| `EnableDeveloperAPI`    | bool | false   | Enables simulation APIs used by hook evaluation      |
| `EnableTxnEvalTracer`   | bool | false   | Enables transaction evaluation tracing               |
| `EndpointAddress`       | str  | ""      | API listen address (e.g. `0.0.0.0:8081`)             |

---

## Examples

### End-to-End: Deploy and Query a Counter Hook

This complete example starts the sandbox, deploys a counter hook, sends a transaction to produce blocks, and reads the counter value.

```bash
# 1. Start the sandbox
docker compose -f docker-compose.nimbus.yml up -d

# 2. Wait for healthy
until curl -sf \
  -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  http://localhost:4101/v2/status > /dev/null 2>&1; do
  sleep 1
done

# 3. Compile a counter hook (increments an 8-byte counter each block)
PROGRAM=$(docker exec nimbus_sandbox_algod goal clerk compile /dev/stdin -o /dev/stdout <<'TEAL' | base64
#pragma version 11
txna ApplicationArgs 0
len
bz init
txna ApplicationArgs 0
btoi
int 1
+
itob
log
int 1
return
init:
int 1
itob
log
int 1
return
TEAL
)

# 4. Register the hook
curl -s -X POST \
  -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  -d "{\"id\":\"counter\",\"program\":\"$PROGRAM\"}" \
  http://localhost:4101/v2/nimbus/hooks

# 5. Send a payment to trigger block production (DevMode produces a block per txn)
WALLET_TOKEN="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
ACCOUNTS=$(curl -s \
  -H "X-Algo-API-Token: $WALLET_TOKEN" \
  http://localhost:4103/v2/accounts | python3 -c "import sys,json; print(json.load(sys.stdin)['accounts'][0]['address'])")

docker exec nimbus_sandbox_algod goal clerk send \
  -a 0 \
  -f "$ACCOUNTS" \
  -t "$ACCOUNTS" \
  -d /algod/data

# 6. Read the counter state
sleep 2
curl -s \
  -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  http://localhost:4101/v2/nimbus/hooks/counter/state

# 7. Verify the receipt chain
curl -s \
  -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  http://localhost:4101/v2/nimbus/hooks/counter/verify

# 8. View history
curl -s \
  -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  http://localhost:4101/v2/nimbus/hooks/counter/history
```

### Hook with Initial State

Pass a seed value that the hook receives on its first evaluation:

```bash
# Base64-encode the initial state
SEED=$(echo -n "hello world" | base64)

curl -s -X POST \
  -H "X-Algo-API-Token: $NIMBUS_TOKEN" \
  -d "{\"id\":\"seeded-hook\",\"program\":\"$PROGRAM\",\"initial-state\":\"$SEED\"}" \
  "$NIMBUS_URL/v2/nimbus/hooks"
```

The hook's first `ApplicationArgs[0]` will contain the bytes `hello world`.

### Hook with Origin Backfill

For hooks that need to process the entire chain from genesis:

```bash
curl -s -X POST \
  -H "X-Algo-API-Token: $NIMBUS_TOKEN" \
  -d "{\"id\":\"full-history\",\"program\":\"$PROGRAM\",\"require-origin\":true}" \
  "$NIMBUS_URL/v2/nimbus/hooks"
```

This requires the node to be running in archival mode. The hook will be evaluated against every block from round 1 to the current round before it begins processing new blocks.

### Reading State from JavaScript

```javascript
const NIMBUS_URL = "http://localhost:4101";
const TOKEN = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

// Get latest state
const response = await fetch(`${NIMBUS_URL}/v2/nimbus/hooks/counter/state`, {
  headers: { "X-Algo-API-Token": TOKEN }
});
const data = await response.json();
console.log("Round:", data.round);
console.log("State (base64):", data.state);

// Decode 8-byte big-endian counter
const stateBytes = Uint8Array.from(atob(data.state), c => c.charCodeAt(0));
const view = new DataView(stateBytes.buffer);
const counter = view.getBigUint64(0);
console.log("Counter value:", counter);

// Verify chain integrity
const verifyResponse = await fetch(`${NIMBUS_URL}/v2/nimbus/hooks/counter/verify`, {
  headers: { "X-Algo-API-Token": TOKEN }
});
const result = await verifyResponse.json();
console.log("Chain valid:", result.valid);
console.log("Entries:", result.entries);
```

### Reading State from Python

```python
import requests
import struct
import base64

NIMBUS_URL = "http://localhost:4101"
TOKEN = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
HEADERS = {"X-Algo-API-Token": TOKEN}

# Get latest state
state = requests.get(f"{NIMBUS_URL}/v2/nimbus/hooks/counter/state", headers=HEADERS).json()
print(f"Round: {state['round']}")

# Decode 8-byte big-endian counter
state_bytes = base64.b64decode(state["state"])
counter = struct.unpack(">Q", state_bytes)[0]
print(f"Counter: {counter}")

# Get history for rounds 0-10
history = requests.get(
    f"{NIMBUS_URL}/v2/nimbus/hooks/counter/history",
    params={"from": 0, "to": 10},
    headers=HEADERS
).json()
for entry in history["history"]:
    value = struct.unpack(">Q", base64.b64decode(entry["state"]))[0]
    print(f"  Round {entry['round']}: {value}")

# Verify chain
result = requests.get(
    f"{NIMBUS_URL}/v2/nimbus/hooks/counter/verify",
    headers=HEADERS
).json()
print(f"Chain valid: {result['valid']}, entries: {result['entries']}")
```

---

## Troubleshooting

### "nimbus hooks disabled"

The node is not running in Nimbus mode. Ensure `EnableNimbusMode` is set to `true` in the node's config. When using Docker, set the `NIMBUS_MODE=1` environment variable.

### "archival mode required for origin hooks"

You tried to create a hook with `require-origin: true` on a non-archival node. Set `Archival: true` in the node config or omit `require-origin` from the request.

### "hook did not emit ARC4 ABI log output"

The hook program did not call `log` during execution. Hooks must emit at least one log message. The last log message becomes the new state.

### Hook state shows an error but the chain continues

This is by design. When a hook evaluation fails, the error is recorded and the receipt chain continues. The state carries forward from the last successful evaluation. Check the `error` field in the state response.

### Container health check fails

The health check queries the Nimbus node API. If it fails:

```bash
# Check container logs
docker logs nimbus_sandbox_algod

# Check both nodes are running inside the container
docker exec nimbus_sandbox_algod goal network status -r /algod
```

### Port conflicts

The default ports (4101, 4102, 4103) may conflict with other services. Edit `docker-compose.nimbus.yml` to change the host-side port mappings.
