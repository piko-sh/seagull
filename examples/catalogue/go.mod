module seagull.sh/seagull/examples/catalogue

go 1.27.0

require (
	github.com/stretchr/testify v1.12.1
	go.yaml.in/yaml/v3 v3.0.5
	seagull.sh/seagull v0.0.0
)

require go.uber.org/goleak v1.3.0 // indirect

replace seagull.sh/seagull => ../..
