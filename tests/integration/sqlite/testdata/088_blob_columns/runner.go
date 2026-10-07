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

	_, err = conn.ExecContext(ctx, `CREATE TABLE files (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		content BLOB NOT NULL,
		size INTEGER NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	err = queries.InsertFile(ctx, db.InsertFileParams{
		ID:      int64(1),
		Name:    "hello.txt",
		Content: []byte("Hello, World!"),
		Size:    int64(13),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertFile 1:", err)
		os.Exit(1)
	}

	err = queries.InsertFile(ctx, db.InsertFileParams{
		ID:      int64(2),
		Name:    "binary.dat",
		Content: []byte{0x00, 0xFF, 0x42},
		Size:    int64(3),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertFile 2:", err)
		os.Exit(1)
	}

	file, err := queries.GetFile(ctx, int64(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetFile:", err)
		os.Exit(1)
	}

	fileNames, err := queries.ListFileNames(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListFileNames:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"file":       file,
		"file_names": fileNames,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
