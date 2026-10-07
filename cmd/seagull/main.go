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

package main

import (
	"os"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

// exit ends the process with a status code; tests stand in for it.
var exit = os.Exit

// main runs the CLI as seagull with the stock drivers and exits with the returned status
// code.
func main() {
	exit(cli.Main(os.Args[1:],
		cli.WithDriver(dialect_engine_clickhouse.Family, "clickhouse"),
		cli.WithDriver(dialect_engine_mysql.Family, "mysql"),
		cli.WithDriver(dialect_engine_postgres.Family, "pgx"),
		cli.WithDriver(dialect_engine_sqlite.Family, "sqlite"),
		cli.WithName("seagull"),
	))
}
