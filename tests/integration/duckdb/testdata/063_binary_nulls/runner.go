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
	if _, err := connection.ExecContext(ctx, "CREATE TABLE binary_values (value BLOB)"); err != nil {
		return err
	}
	queries := db.New(connection)
	for _, value := range [][]byte{nil, {}, {0, 255}} {
		if err := queries.AddValue(ctx, value); err != nil {
			return err
		}
	}
	if err := check(ctx, queries); err != nil {
		return err
	}
	prepared, err := db.Prepare(ctx, connection)
	if err != nil {
		return err
	}
	defer prepared.Close()
	if err := check(ctx, db.New(prepared)); err != nil {
		return fmt.Errorf("prepared: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode("NULL and empty bytes remain distinct")
}

func check(ctx context.Context, queries *db.Queries) error {
	rows, err := queries.ReadValues(ctx)
	if err != nil || len(rows) != 3 {
		return fmt.Errorf("read: %v, %v", rows, err)
	}
	if !rows[0].IsNull || rows[0].Value != nil || rows[1].IsNull || rows[1].Value == nil || len(rows[1].Value) != 0 {
		return fmt.Errorf("NULL and empty distinction: %v", rows)
	}
	var missing []byte
	for _, value := range []any{missing, &missing} {
		count, err := queries.BrowseValues().Where("value", "=", value).Count(ctx)
		if err != nil || count != 0 {
			return fmt.Errorf("runtime nil equality: %d, %v", count, err)
		}
	}
	for _, values := range [][][]byte{{nil}, {nil, {}}, {{}}} {
		want := 1
		if len(values) == 1 && values[0] == nil {
			want = 0
		}
		rows, err := queries.ReadSlice(ctx, db.ReadSliceParams{Values: values})
		if err != nil || len(rows) != want {
			return fmt.Errorf("static binary list: %v, %v", rows, err)
		}
		count, err := queries.BrowseValues().Where("value", "IN", values).Count(ctx)
		if err != nil || count != int64(want) {
			return fmt.Errorf("runtime binary list: %d, %v", count, err)
		}
	}
	return nil
}
