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
		`CREATE TABLE events (id INTEGER PRIMARY KEY)`,
		`INSERT INTO events VALUES (1)`,
	} {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	queries := db.New(connection)
	keywordForms, err := queries.KeywordForms(ctx, 1)
	if err != nil {
		return fmt.Errorf("KeywordForms: %w", err)
	}
	callForms, err := queries.CallForms(ctx, 1)
	if err != nil {
		return fmt.Errorf("CallForms: %w", err)
	}
	output := map[string]any{
		"KeywordForms": map[string]any{
			"time_with_zone_read":  !keywordForms.TimeWithZone.IsZero(),
			"stamp_with_zone_read": !keywordForms.StampWithZone.IsZero(),
			"local_stamp_read":     !keywordForms.LocalStamp.IsZero(),
			"today_read":           !keywordForms.Today.IsZero(),
			"role_name":            keywordForms.RoleName,
			"session_name":         keywordForms.SessionName,
		},
		"CallForms": map[string]any{
			"today_read":   !callForms.Today.IsZero(),
			"user_name":    callForms.UserName,
			"role_name":    callForms.RoleName,
			"session_name": callForms.SessionName,
			"schema_name":  callForms.SchemaName,
			"catalog_read": callForms.CatalogName != "",
		},
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
