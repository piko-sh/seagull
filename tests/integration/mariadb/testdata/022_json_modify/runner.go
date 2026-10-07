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

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

func main() {
	connectionString := os.Getenv("DATABASE_URL")

	conn, err := sql.Open("mysql", connectionString)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	queries := db.New(conn)

	err = queries.InsertSetting(ctx, db.InsertSettingParams{
		Name:   "user_prefs",
		Config: db.JSON(`{"theme": "dark", "language": "en", "notifications": true}`),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertSetting:", err)
		os.Exit(1)
	}

	original, err := queries.GetSetting(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetSetting original:", err)
		os.Exit(1)
	}
	err = queries.SetConfigField(ctx, db.SetConfigFieldParams{
		Config:  "$.font_size",
		Config2: db.JSON("14"),
		ID:      int32(1),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SetConfigField:", err)
		os.Exit(1)
	}

	afterSet, err := queries.GetSetting(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetSetting after set:", err)
		os.Exit(1)
	}
	err = queries.ReplaceConfigField(ctx, db.ReplaceConfigFieldParams{
		Config: db.JSON("light"),
		ID:     int32(1),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ReplaceConfigField:", err)
		os.Exit(1)
	}

	afterReplace, err := queries.GetSetting(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetSetting after replace:", err)
		os.Exit(1)
	}
	err = queries.RemoveConfigField(ctx, db.RemoveConfigFieldParams{
		Config: "$.notifications",
		ID:     int32(1),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "RemoveConfigField:", err)
		os.Exit(1)
	}

	afterRemove, err := queries.GetSetting(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetSetting after remove:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"original":      original,
		"after_set":     afterSet,
		"after_replace": afterReplace,
		"after_remove":  afterRemove,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
