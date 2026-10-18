# Runcoil server

Runcoil runs background Tasks for an application and keeps the state of every
Run. This repository is the server: a [ConnectRPC](https://connectrpc.com) API
over PostgreSQL.

The application's own process runs the Task code. A worker, built with the
[Go SDK](https://github.com/tuanvumaihuynh/runcoil-go), connects out to the
server, registers its Tasks, and receives Runs. A caller invokes a Task through
the API and reads the Run.

**Status: pre-release.** The API and the schema may change before version 1.0.
See [Limits of this version](#limits-of-this-version).

## What it does

- **Runs and Steps.** A Task handler runs straight through. Each Step records
  its outcome, so a retried Run replays the Steps that succeeded and does not
  run them again.
- **Retries.** A failed Step is tried again with a delay; the policy is set per
  Task or per Step, with server defaults.
- **Schedules.** A Task can run on a cron expression, at an interval, or at
  times of day, in UTC. A schedule can skip a tick while the previous Run is
  unfinished.
- **Lost workers.** A worker renews a lease on each Run it holds. When a worker
  dies, the lease expires and the Run goes to another worker.
- **Idempotent invokes.** Every `Invoke` and `Rerun` carries an idempotency
  key. A repeat with the same key and input returns the first Run.
- **Rerun.** A finished Run can be run again from its start or from a chosen
  Step.

## Requirements

- Go 1.26 and `make`
- Docker, for the local PostgreSQL 18

The tools (golangci-lint, sqlc, goose) run through `go run` at pinned versions.
Nothing is installed globally.

## Quick start

Clone this repository, start PostgreSQL, create the tables, and start the server:

```sh
git clone https://github.com/tuanvumaihuynh/runcoil.git
cd runcoil
make dc-up          # PostgreSQL 18 on port 5432
make run-migrate    # create the tables; running it again changes nothing
make run-runcoil    # the server on port 7070, with the token dev-token
```

Start a worker in a second terminal from a checkout of the
[Go SDK](https://github.com/tuanvumaihuynh/runcoil-go). It can be cloned anywhere;
the server and SDK do not need sibling directories:

```sh
git clone https://github.com/tuanvumaihuynh/runcoil-go.git
cd runcoil-go
go run ./examples/hello    # worker ID "hello", one Task "hello"
```

Invoke the Task. The input and the output are JSON, sent as base64:

```sh
curl -s -X POST http://localhost:7070/runcoil.v1.RunService/Invoke \
  -H 'Authorization: Bearer dev-token' -H 'Content-Type: application/json' \
  -d '{"workerId":"hello","taskId":"hello","input":"eyJuYW1lIjoiQWRhIn0=","idempotencyKey":"hello-1"}'
# {"runId":"..."}
```

Read the Run, with the ID from the answer:

```sh
curl -s -X POST http://localhost:7070/runcoil.v1.RunService/WaitRun \
  -H 'Authorization: Bearer dev-token' -H 'Content-Type: application/json' \
  -d '{"runId":"<run id>","includeSteps":true}'
```

`eyJuYW1lIjoiQWRhIn0=` is `{"name":"Ada"}`. The Run's `output` decodes to
`{"greeting":"hello, Ada"}`.

If the server stops at start with `relation "schedules" does not exist`, the
tables are missing: run `make run-migrate`.

## API

Every request needs `Authorization: Bearer <token>`, with the token the server
was started with (`ENGINE__TOKEN`). The schema is in
[runcoil-proto](https://github.com/tuanvumaihuynh/runcoil-proto).

| Service | Calls | Used by |
| --- | --- | --- |
| `runcoil.v1.RunService` | `Invoke`, `GetRun`, `WaitRun`, `ListRuns`, `Rerun`, `ListTasks` | Callers |
| `runcoil.v1.WorkerService` | `Register`, `Listen`, `Heartbeat`, `AckDispatch`, `RecordStep`, `FinishRun` | The SDK |

`Invoke` and `Rerun` need an `idempotencyKey`. A repeat with the same key and
input returns the first Run, marked `deduplicated`. The same key with other
input is refused with `IDEMPOTENCY_CONFLICT`. A key is held for 24 hours by
default.

## Configuration

Each binary embeds its defaults in `config.yml`. Sources, in order: the
defaults, the `--config` file if given, then environment variables. Environment
variables carry no prefix, and a double underscore nests keys: `POSTGRES__HOST`
sets `postgres.host`. Variables without a double underscore are ignored.
`.env.example` lists every key with its default.

Before the server is reachable by anything but your own machine:

- **Set your own `ENGINE__TOKEN`.** The server has no default token and refuses
  to start without one. `make run-runcoil` and `.env.example` use `dev-token`,
  which is for local work only.
- **Put TLS in front.** The server speaks plain HTTP (HTTP/1.1 and HTTP/2
  without TLS) on every interface, so the token travels in clear text unless a
  proxy or load balancer terminates TLS.
- **Set the PostgreSQL user, password, and `POSTGRES__SSL_MODE`.** The defaults
  are for the local container.

## Logs

The server writes one line per event to stdout. The default level is `info`,
which shows the rare, operational events:

- a worker registered (worker ID, instance, Task count, slots, SDK name and
  version, and whether this is the first registration of the instance in this
  process), and the schedule changes it caused (added, changed, removed);
- a worker stream attached, and detached with its cause (closed by the worker,
  replaced by a new stream, ended for silence, send timeout, server stopping);
- an instance evicted for silence; a Run failed by the server (start deadline,
  worker wait limit, attempt lost with no tries left); a lease swept, with its
  outcome;
- finished Runs and expired idempotency keys removed by the cleanup jobs;
- the start line, with the addresses and intervals that matter to an operator
  (no token).

Debug shows each RPC when it ends (procedure, duration, result code, Runcoil
error code), the Run lifecycle (created, deduplicated, dispatched, acked, Step
recorded, retry scheduled, released, finished, given back), and scheduler
ticks. Logs never carry tokens, inputs, or outputs.

```sh
LOG__LEVEL=debug make run-runcoil   # debug lines
LOG__FORMAT=json make run-runcoil   # one JSON object per line
```

`LOG__FORMAT` is `text` by default: coloured, for a terminal. Use `json` in a
container or behind a log collector.

## Health and shutdown

Two health checks share the server's port and need no token:

- `GET /livez` answers `200 ok` whenever the HTTP server answers.
- `GET /readyz` answers `200 ok` when the server takes new work. Otherwise it
  answers `503` with the reason as its body: `starting`, `shutting down`, or
  `database unavailable` when a database ping does not answer within 2 seconds.

The first `SIGINT` or `SIGTERM` starts a drain. The server refuses new Runs and
new worker registrations, and keeps accepting the reports that a worker makes on
the Runs it holds, so the workers can finish them. The server stops when those
Runs are finished, or when `engine.shutdown_timeout` passes (`10s` by default).
A Run that is still held at that point keeps its lease, and it is handled like
any lost lease after the restart.

A second signal during the drain exits the server at once, without waiting for
the drain to end.

## Limits of this version

- **One server process.** Running several servers against one database is not
  supported: the record of connected workers lives in the process.
- **One shared token**, and no TLS in the server itself.
- **No SDK call to invoke a Task.** A caller uses the API directly, as in the
  quick start.
- **Schedules are in UTC**, declared in worker code only. A tick that passes
  while the server is down creates no Run.
- **No event triggers and no web interface** yet.
- **Finished Runs are removed** 7 days after they finish by default
  (`WORKER__CLEANUP__RUN_RETENTION`).

## Commands

| Command | Does |
| --- | --- |
| `make run-runcoil` | Runs the server |
| `make run-migrate` | Applies migrations, then exits |
| `make migrate-up` / `migrate-down` / `migrate-status` | goose, against the local database |
| `make migrate-create name=<name>` | New SQL migration |
| `make gen-sqlc` | Regenerates the sqlc code; commit the result |
| `make test` / `make test-cov` | Unit tests |
| `make lint` | golangci-lint v2 |
| `make vuln` | govulncheck; reports stdlib findings on an older Go 1.26 patch, so use the newest one as CI does |
| `make dc-up` / `make dc-down` | Local PostgreSQL |

## Layout

- `cmd/runcoil`, `cmd/migrate`: the binaries
- `internal/engine`: the core, the dispatcher and the scheduler, and the ConnectRPC handlers
- `internal/worker`: the server's own background jobs: the cleanup of finished Runs and expired keys
- `internal/domain`: Runs, Tasks, Steps, schedules, retry rules; no infrastructure
- `internal/apierr`: the error vocabulary; the engine translates it at one place
- `internal/storage/postgres`: stores, the migration, and the sqlc queries in `sqlc/`
- `internal/log`, `pkg/cmdutil`: logging, configuration loading
- `pkg/service`: starts the long-running parts and stops them on a signal
- `docker/app`: a Dockerfile for each binary

The schema module `github.com/tuanvumaihuynh/runcoil-proto` is pinned to a
published source revision in `go.mod`. This repository builds independently,
without a sibling schema checkout or `go.work`.

Build the local images from this repository's root:

```sh
docker build -f docker/app/runcoil.dockerfile -t runcoil:local .
docker build -f docker/app/migrate.dockerfile -t runcoil-migrate:local .
```

No prebuilt container image or tagged release is published yet.

## License

Apache License 2.0. See [LICENSE](LICENSE).
