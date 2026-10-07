module seagull.sh/seagull/examples/mysql

go 1.27.0

require (
	github.com/cockroachdb/apd/v3 v3.2.3
	github.com/go-sql-driver/mysql v1.10.0
	seagull.sh/seagull v0.0.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
)

replace seagull.sh/seagull => ../..
