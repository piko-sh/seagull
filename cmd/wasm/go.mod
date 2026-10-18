module seagull.sh/seagull/cmd/wasm

go 1.27.0

require (
	github.com/stretchr/testify v1.12.1
	piko.sh/goastutil v0.2.0
	piko.sh/safedisk v0.1.0
	seagull.sh/seagull v0.0.0-alpha.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	piko.sh/safeconv v0.1.0 // indirect
)

replace seagull.sh/seagull => ../..
