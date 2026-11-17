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
	if _, err := connection.ExecContext(ctx, `CREATE TABLE blobs (id BIGINT PRIMARY KEY, data BLOB NOT NULL, tags VARCHAR[] NOT NULL, nums BIGINT[] NOT NULL, maybe BLOB)`); err != nil {
		return err
	}
	queries := db.New(connection)
	if err := queries.AddBlob(ctx, db.AddBlobParams{ID: 1}); err != nil {
		return fmt.Errorf("AddBlob: %w", err)
	}
	if err := queries.AddBlobs(ctx, []db.AddBlobsParams{{ID: 2}, {ID: 3}}); err != nil {
		return fmt.Errorf("AddBlobs: %w", err)
	}
	if err := queries.CopyBlobs(ctx, []db.CopyBlobsParams{{ID: 4}, {ID: 5}}); err != nil {
		return fmt.Errorf("CopyBlobs: %w", err)
	}
	blobs, err := queries.ListBlobs(ctx)
	if err != nil {
		return fmt.Errorf("ListBlobs: %w", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"ListBlobs": blobs})
}
