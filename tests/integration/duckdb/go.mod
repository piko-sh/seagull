module seagull.sh/seagull/tests/integration/duckdb

go 1.27.0

require (
	github.com/duckdb/duckdb-go/v2 v2.10505.0
	github.com/stretchr/testify v1.12.1
	seagull.sh/seagull v0.0.0
	seagull.sh/seagull/cmd/sgl v0.0.0
	seagull.sh/seagull/tests/integration/internal/harness v0.0.0
)

require (
	github.com/apache/arrow-go/v18 v18.5.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/duckdb/duckdb-go-bindings v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-amd64 v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-arm64 v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-amd64 v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-arm64 v0.10505.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/windows-amd64 v0.10505.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/telemetry v0.0.0-20260908163034-4bcc4b2ee518 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	piko.sh/goastutil v0.2.0 // indirect
	piko.sh/safeconv v0.1.0 // indirect
	piko.sh/safedisk v0.1.0 // indirect
)

replace (
	seagull.sh/seagull/cmd/sgl => ../../../cmd/sgl
	seagull.sh/seagull/tests/integration/internal/harness => ../internal/harness
)
