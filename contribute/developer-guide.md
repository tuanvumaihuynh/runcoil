# Developer guide

## Requirements

- Go 1.27
- [Task](https://taskfile.dev)
- Docker, for Compose and the image

The Go tools (golangci-lint, govulncheck, goose, buf) are pinned in `tools/go.mod` and run
through `go tool`. Nothing else is installed globally.

## Development

```sh
task setup    # download the modules and build the Go tools
task build    # build the server
task test     # tests with the race detector
task lint     # golangci-lint
task tidy     # fail when go.mod or go.sum is not tidy
task vuln     # govulncheck
task check    # lint, test, tidy and vuln
```

The tests open no database. Every tool call runs with `GOWORK=off`, because
`go tool -modfile` does not work in workspace mode.

## Database

The migrations are goose SQL files in `internal/store/postgres/migrations`,
embedded in the server and in `runcoil-migrate`, a binary that applies them and
exits. With `auto-migrate` set, the server applies them at boot; Compose
sets it. The tasks run goose against the PostgreSQL
of Compose, or against `GOOSE_DBSTRING` when it is set.

The queries are sqlc files in `internal/store/postgres/queries`, one per table;
the generated Go code is committed in `internal/store/postgres/gen`.

```sh
task sqlc-gen                    # generate the Go code after changing a query or a migration
task migrate-up                  # apply every pending migration
task migrate-down                # roll back the last one
task migrate-reset               # roll back every migration
task migrate-status              # show which are applied
task migrate-create -- <name>    # add a migration
```

## Protocol

The worker protocol is in `proto/`, a Buf module and its own Go module,
`github.com/runcoil/runcoil/proto`. The generated Go code is committed under
`proto/gen/go`. The server requires `proto` at a release tag and replaces it with
`./proto`, so it always builds the protocol of its own commit.

Each merge to `main` that changes `proto/` tags a new patch version,
`proto/vX.Y.Z`, through `proto-release.yaml`; `#minor` or `#major` in the commit
message bumps that part instead. The Go SDK and other users import a tag.

```sh
task proto:gen      # generate the Go code after changing a .proto file
task proto:format   # format the protos
task proto:check    # lint, format check, generated-code check, build and tidy
```

## Go SDK

The Go SDK is `sdks/go`, its own Go module, `github.com/runcoil/runcoil/sdks/go`, on
Go 1.26. It requires `proto` at a release tag; `go.work` builds it against `./proto`
for work across modules, and its tasks run with `GOWORK=off`, as CI does.

```sh
task sdk-go:build   # build and vet the SDK and its examples
task sdk-go:test    # tests with the race detector, against an in-process fake server
task sdk-go:check   # lint, test, tidy and vuln
```

[`sdks/go/README.md`](../sdks/go/README.md) describes the SDK itself.

`sdks/go/examples/hello` is a worker of the app `hello` with one Task, `greet`, of
three Steps. It reads `RUNCOIL_URL` (the dispatcher port, `http://localhost:7070`)
and `RUNCOIL_API_KEY`. Make a key in the dashboard at `http://localhost:8080`:
sign in as `admin`/`admin`, then Project settings › API keys › New API key. The
dashboard shows the key once.

```sh
cd sdks/go
RUNCOIL_URL=http://localhost:7070 RUNCOIL_API_KEY=<your key> go run ./examples/hello
```

## Running

```sh
docker compose up -d     # PostgreSQL 18, the migrations, then the server
docker compose down
task run                 # the server on the host, against the PostgreSQL of Compose
```

The server listens on two ports:

- `8080`, the API port, with the health checks of the whole process:
  - `/livez`: the process is alive
  - `/readyz`: the database answers, the dispatcher takes workers, and the server is
    not shutting down
- `7070`, the dispatcher port, with the worker protocol, `runcoil.v1.WorkerService`

## Configuration

Each key is a flag (`--postgres.host`), an environment variable with the same
name in upper case (`POSTGRES_HOST`), or a key in a YAML file passed with
`--config`, lowest first: file, environment, flag. Each binary has a
`config.example.yml` and an `.env.example` in its `cmd/` directory that list every
key with its default. No binary reads `.env`; pass flags to `task run` after `--`
to change a key.

## Image

Build the images from the root of the repository:

```sh
docker build -f docker/runcoil/runcoil.dockerfile -t runcoil:local .
docker build -f docker/runcoil/runcoil-migrate.dockerfile -t runcoil-migrate:local .
```
