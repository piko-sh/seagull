module seagull.sh/seagull/tests/integration/sqlite

go 1.27.0

require (
	github.com/stretchr/testify v1.12.1
	modernc.org/sqlite v1.56.0
	seagull.sh/seagull v0.0.0
	seagull.sh/seagull/tests/integration/internal/harness v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	modernc.org/libc v1.74.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	piko.sh/goastutil v0.2.0 // indirect
	piko.sh/safeconv v0.1.0 // indirect
	piko.sh/safedisk v0.1.0 // indirect
)

replace seagull.sh/seagull/tests/integration/internal/harness => ../internal/harness
