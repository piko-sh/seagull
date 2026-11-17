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
	"case_runner/db"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	connection, err := sql.Open(databaseDriver, "")
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetMaxOpenConns(1)
	ctx := context.Background()
	for _, statement := range []string{
		`CREATE TABLE readings ( id INTEGER PRIMARY KEY, delta INTEGER NOT NULL, count UINTEGER NOT NULL, total UBIGINT NOT NULL, offset_value BIGINT NOT NULL, label VARCHAR NOT NULL )`,
		`INSERT INTO readings VALUES (1, -5, 7, 18446744073709551615, -9, 'gull'), (2, 3, 4000000000, 5, 9, 'tern')`,
	} {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	queries := db.New(connection)
	output := map[string]any{}
	mixedColumns, err := queries.MixedColumns(ctx)
	if err != nil {
		return fmt.Errorf("MixedColumns: %w", err)
	}
	output["MixedColumns"] = mixedColumns
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
