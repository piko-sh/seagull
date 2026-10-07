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
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

const schema = `CREATE TABLE gulls (
    id       INTEGER PRIMARY KEY,
    name     VARCHAR NOT NULL,
    wingspan DOUBLE,
    weight   DECIMAL(6, 2) NOT NULL,
    lifespan INTERVAL NOT NULL,
    notes    JSON
);
INSERT INTO gulls VALUES
    (1, 'herring', 1.40, 1.25, INTERVAL 30 YEAR, '{"colour": "grey"}'),
    (2, 'kittiwake', NULL, 0.40, INTERVAL 6 MONTH, NULL);
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err = conn.ExecContext(ctx, schema); err != nil {
		return err
	}
	queries := db.New(conn)
	lateral, err := queries.LateralAliases(ctx)
	if err != nil {
		return fmt.Errorf("LateralAliases: %w", err)
	}
	rows := make([]map[string]any, 0, len(lateral))
	for _, row := range lateral {
		rows = append(rows, map[string]any{
			"row":         row,
			"sum":         row.NextID + row.Doubled,
			"shout":       row.Shout + "?",
			"mass":        row.Mass.String(),
			"double_mass": row.DoubleMass.String(),
			"long_lived":  !row.LongLived,
		})
	}
	before, err := queries.ColumnBeforeAlias(ctx)
	if err != nil {
		return fmt.Errorf("ColumnBeforeAlias: %w", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"lateral": rows, "column_before_alias": before})
}
