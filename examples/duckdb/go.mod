module seagull.sh/seagull/examples/duckdb

go 1.27.0

require (
	github.com/duckdb/duckdb-go/v2 v2.20000.0
	github.com/google/uuid v1.6.0
	seagull.sh/seagull v0.0.0
)

require (
	github.com/apache/arrow-go/v18 v18.9.0 // indirect
	github.com/duckdb/duckdb-go-bindings v0.20000.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/goccy/go-json v0.12.0 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/klauspost/compress v1.20.4 // indirect
	github.com/klauspost/cpuid/v2 v2.4.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.duckdb.org/libduckdb/darwin-amd64 v0.20000.0 // indirect
	go.duckdb.org/libduckdb/darwin-arm64 v0.20000.0 // indirect
	go.duckdb.org/libduckdb/linux-amd64 v0.20000.0 // indirect
	go.duckdb.org/libduckdb/linux-arm64 v0.20000.0 // indirect
	go.duckdb.org/libduckdb/windows-amd64 v0.20000.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	golang.org/x/exp v0.0.0-20261117202309-7a49493bfd90 // indirect
	golang.org/x/sys v0.50.0 // indirect
)

replace seagull.sh/seagull => ../..
