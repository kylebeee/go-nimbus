
# go-nimbus

A fork of [go-algorand](https://github.com/algorand/go-algorand) that adds programmable hooks with a cryptographic receipt chain. Nimbus hooks are AVM programs that run against every block, producing derived state with tamper-evident receipts.

## Quick Start

Start a local Nimbus network with Docker:

```bash
docker compose -f docker-compose.nimbus.yml up -d
```

| Service     | Host Port | Description       |
|-------------|-----------|-------------------|
| Nimbus API  | 4101      | Hook management   |
| KMD         | 4102      | Key management    |
| Relay API   | 4103      | Consensus node    |

All endpoints use the dev token: `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`

```bash
# Check status
curl -s -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  http://localhost:4101/v2/status

# List hooks
curl -s -H "X-Algo-API-Token: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  http://localhost:4101/v2/nimbus/hooks
```

## What is a Hook?

A hook is a compiled AVM (TEAL) program registered with a Nimbus node. Every time the node receives a new block, it evaluates each hook by simulating an application call. The hook receives the previous round's state as input (`ApplicationArgs[0]`) and produces new state as output (its last log message).

Each evaluation generates a cryptographic receipt that binds the output to the block, forming a tamper-evident chain.

Hooks run with all standard AVM constraints removed -- unlimited opcode budget, unlimited log size and count, no app args limits, and no call depth limit. This allows hooks to perform arbitrarily complex computations. See [nimbus/NIMBUS.md](nimbus/NIMBUS.md) for the full list of removed constraints.

## Writing Hooks

Hooks can be written in TEAL or Algorand TypeScript using the [`@akitafoundation/nimbus-hooks`](https://github.com/kylebeee/nimbus-hooks) package:

```typescript
import { bytes, btoi, itob, Uint64 } from '@algorandfoundation/algorand-typescript'
import { HookContract } from '@akitafoundation/nimbus-hooks'

class BlockCounter extends HookContract {
  public program(previousState: bytes): bytes {
    if (previousState.length > 0) {
      const prev = btoi(previousState)
      return itob(prev + Uint64(1))
    }
    return itob(Uint64(1))
  }
}
```

## API Endpoints

| Method   | Path                                | Auth   | Description                   |
|----------|-------------------------------------|--------|-------------------------------|
| `GET`    | `/v2/nimbus/hooks`                  | Public | List all hooks                |
| `POST`   | `/v2/nimbus/hooks`                  | Admin  | Register a new hook           |
| `GET`    | `/v2/nimbus/hooks/:id`              | Public | Get hook metadata             |
| `DELETE` | `/v2/nimbus/hooks/:id`              | Admin  | Delete a hook                 |
| `GET`    | `/v2/nimbus/hooks/:id/state`        | Public | Get latest hook state         |
| `GET`    | `/v2/nimbus/hooks/:id/history`      | Public | Get state history (from/to)   |
| `GET`    | `/v2/nimbus/hooks/:id/verify`       | Public | Verify receipt chain          |

## Documentation

See [nimbus/NIMBUS.md](nimbus/NIMBUS.md) for full documentation including:

- Manual node setup (without Docker)
- TEAL hook examples with compilation instructions
- Detailed API reference with curl examples
- Receipt chain cryptography and verification
- Configuration reference
- End-to-end walkthroughs in JavaScript and Python
- Troubleshooting

## Building from Source

```bash
git clone https://github.com/kylebeee/go-nimbus
cd go-nimbus
./scripts/configure_dev.sh
make build
```

## Running Tests

```bash
# All nimbus tests
go test ./nimbus/...

# Hook API tests
go test ./daemon/algod/api/server/v2/test/ -run TestNimbus

# Full test suite
make test
```

## Docker Commands

```bash
# Start
docker compose -f docker-compose.nimbus.yml up -d

# Rebuild after code changes
docker compose -f docker-compose.nimbus.yml up -d --build

# Stop (preserves data)
docker compose -f docker-compose.nimbus.yml down

# Stop and wipe data
docker compose -f docker-compose.nimbus.yml down -v

# View logs
docker logs nimbus_sandbox_algod

# Check both internal nodes
docker exec nimbus_sandbox_algod goal network status -r /algod
```

## Upstream

This is a fork of [algorand/go-algorand](https://github.com/algorand/go-algorand). To sync with upstream:

```bash
git fetch upstream
git merge upstream/master
```

## License

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](COPYING)

Copyright (C) 2019-2026, Algorand Inc.
