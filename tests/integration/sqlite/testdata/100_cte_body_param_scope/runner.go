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

	"case_runner/db"
)

func main() {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	schema := []string{
		`CREATE TABLE content_media_folders (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE content_media_folder_versions (id INTEGER PRIMARY KEY, media_folder_id INTEGER NOT NULL, status TEXT NOT NULL)`,
	}
	for _, ddl := range schema {
		if _, err = conn.ExecContext(ctx, ddl); err != nil {
			fmt.Fprintln(os.Stderr, "schema:", err)
			os.Exit(1)
		}
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO content_media_folders (id, name) VALUES (1, 'Photos'), (2, 'Docs')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed folders:", err)
		os.Exit(1)
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO content_media_folder_versions (id, media_folder_id, status) VALUES `+
		`(10, 1, 'draft'), (20, 1, 'published'), (30, 1, 'archived'), (40, 2, 'draft')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed versions:", err)
		os.Exit(1)
	}

	queries := db.New(conn)

	// Latest version of folder 1 before version 30 is version 20 ('published'): the CTE picks
	// MAX(id) where media_folder_id=1 and id<30, i.e. 20, then the main query joins it back.
	row, err := queries.GetLatestFolderVersion(ctx, db.GetLatestFolderVersionParams{
		BeforeVersionID: int64(30),
		FolderID:        int64(1),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetLatestFolderVersion:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"latest_before_30": row,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
