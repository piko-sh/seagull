module seagull.sh/seagull

go 1.27.0

require (
	github.com/stretchr/testify v1.12.1
	go.uber.org/goleak v1.3.0
	go.yaml.in/yaml/v3 v3.0.6
	golang.org/x/tools v0.53.0
	piko.sh/goastutil v0.2.0
	piko.sh/safeconv v0.1.0
	piko.sh/safedisk v0.3.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
)

tool golang.org/x/tools/go/analysis/passes/fieldalignment/cmd/fieldalignment
