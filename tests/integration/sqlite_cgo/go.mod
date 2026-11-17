module seagull.sh/seagull/tests/integration/sqlite_cgo

go 1.27.0

require (
	github.com/mattn/go-sqlite3 v1.14.53
	github.com/stretchr/testify v1.12.1
	seagull.sh/seagull v0.0.0
	seagull.sh/seagull/tests/integration/internal/harness v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/tools v0.53.0 // indirect
	piko.sh/goastutil v0.2.0 // indirect
	piko.sh/safeconv v0.1.0 // indirect
	piko.sh/safedisk v0.3.0 // indirect
)

replace seagull.sh/seagull/tests/integration/internal/harness => ../internal/harness
