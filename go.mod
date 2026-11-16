module github.com/runcoil/runcoil

go 1.27

require (
	connectrpc.com/connect/v2 v2.0.2
	connectrpc.com/validate v0.9.0
	github.com/getkin/kin-openapi v0.153.0
	github.com/jackc/pgx/v5 v5.12.0
	github.com/lmittmann/tint v1.2.1
	github.com/oapi-codegen/nethttp-middleware v1.3.1
	github.com/oapi-codegen/runtime v1.9.0
	github.com/pressly/goose/v3 v3.30.0
	github.com/robfig/cron/v3 v3.0.1
	github.com/runcoil/runcoil/proto v0.1.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/spf13/pflag v1.1.0
	github.com/stretchr/testify v1.12.1
	go.yaml.in/yaml/v3 v3.0.6
	golang.org/x/crypto v0.59.0
	golang.org/x/text v0.44.0
	google.golang.org/protobuf v1.36.12
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.12-20260825204119-511051f7f437.2 // indirect
	buf.build/go/protovalidate v1.4.0 // indirect
	cel.dev/cel-go v0.32.0 // indirect
	cel.dev/expr v0.25.3 // indirect
	connectrpc.com/connect v1.21.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/go-openapi/jsonpointer v1.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v4 v4.0.0 // indirect
	golang.org/x/exp v0.0.0-20261109185158-a52e3aef5920 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20261110000943-09e21b237f2c // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c // indirect
)

replace github.com/runcoil/runcoil/proto => ./proto
