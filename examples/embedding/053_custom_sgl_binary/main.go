// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

// Command 053_custom_sgl_binary is sgl with a team's own additions, built on
// seagull.sh/seagull/cmd/sgl/cli: SQLite with tighter parser limits by default, a
// database/sql emitter that also writes query_index.go, and the pure-Go SQLite driver for
// migrate, seed and schema diff.
//
// Build it with `go build -o sgl .` and use it as you would the stock sgl, or run it in
// place with a command: `go run . generate`, `go run . vet`, `go run . migrate up`. Like
// sgl itself, plain `go run .` with no command only prints the usage.
package main

import (
	"os"

	_ "modernc.org/sqlite"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
)

const (
	// sqlite is both the dialect name sgl.yaml selects and the name modernc.org/sqlite
	// registers its driver under.
	sqlite = "sqlite"

	// defaultMaxParseDepth is this sgl's expression nesting limit when sgl.yaml sets none.
	defaultMaxParseDepth = 64

	// defaultMaxTokensPerStatement is this sgl's statement size limit when sgl.yaml sets
	// none.
	defaultMaxTokensPerStatement = 20_000
)

func main() {
	os.Exit(cli.Main(os.Args[1:], options()...))
}

// options are what this sgl adds to the stock one. A name the stock sgl already knows,
// such as the sqlite dialect or the database/sql driver, is replaced.
//
// Returns []cli.Option which registers the dialect, the emitter and the driver.
func options() []cli.Option {
	return []cli.Option{
		cli.WithDialect(sqlite, strictSQLite),
		cli.WithEmitter("database/sql", indexedDatabaseSQL),
		cli.WithDriver(sqlite, sqlite),
	}
}

// strictSQLite builds the SQLite dialect for `dialect: sqlite`, with tighter parser
// limits than the stock defaults unless sgl.yaml's dialect_options sets them.
//
// Takes options (dialect_registry.Options) which holds the database's dialect_options.
//
// Returns dialect.Dialect which is the configured dialect.
// Returns error when an option does not apply to SQLite.
func strictSQLite(options dialect_registry.Options) (dialect.Dialect, error) {
	if options.MaxParseDepth == 0 {
		options.MaxParseDepth = defaultMaxParseDepth
	}
	if options.MaxTokensPerStatement == 0 {
		options.MaxTokensPerStatement = defaultMaxTokensPerStatement
	}
	return dialect_registry.New(sqlite, options)
}

// indexedDatabaseSQL builds the emitter for `go.driver: database/sql`: the stock
// database/sql emitter, honouring go.prepared, wrapped so it also writes query_index.go.
//
// Takes request (*cli.EmitterRequest) which describes the database's dialect and
// settings.
//
// Returns emit.EmitterPort which generates the package.
// Returns error which is always nil.
func indexedDatabaseSQL(request *cli.EmitterRequest) (emit.EmitterPort, error) {
	base := emit_driver_databasesql.New(emit_driver_databasesql.WithPrepared(request.Prepared))
	return indexEmitter{base: base}, nil
}
