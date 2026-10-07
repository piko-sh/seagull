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

package drivers

import (
	"maps"
	"slices"

	_ "github.com/ClickHouse/clickhouse-go/v2" // registers the clickhouse driver
	_ "github.com/go-sql-driver/mysql"         // registers the mysql driver
	_ "github.com/jackc/pgx/v5/stdlib"         // registers the pgx driver
	_ "modernc.org/sqlite"                     // registers the sqlite driver

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

// stock maps each dialect family to the database/sql driver name the stock binary
// registers for it.
var stock = map[string]string{
	dialect_engine_postgres.Family:   "pgx",
	dialect_engine_mysql.Family:      "mysql",
	dialect_engine_sqlite.Family:     "sqlite",
	dialect_engine_clickhouse.Family: "clickhouse",
}

// Options returns one cli.WithDriver option per stock dialect family, so every dialect of
// that family, stock or custom, opens the stock driver unless a later option names
// another.
//
// Returns []cli.Option which holds the options in family order.
func Options() []cli.Option {
	families := slices.Sorted(maps.Keys(stock))
	options := make([]cli.Option, 0, len(families))
	for _, family := range families {
		options = append(options, cli.WithDriver(family, stock[family]))
	}
	return options
}
