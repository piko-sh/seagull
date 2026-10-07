module seagull.sh/seagull/tests/integration/postgres

go 1.27.0

require (
	github.com/fergusstrange/embedded-postgres v1.34.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/stretchr/testify v1.12.1
	seagull.sh/seagull v0.0.0
	seagull.sh/seagull/tests/integration/internal/harness v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	piko.sh/goastutil v0.2.0 // indirect
	piko.sh/safeconv v0.1.0 // indirect
	piko.sh/safedisk v0.1.0 // indirect
)

replace seagull.sh/seagull/tests/integration/internal/harness => ../internal/harness
