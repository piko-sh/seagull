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
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	connection, err := sql.Open(databaseDriver, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer connection.Close()
	ctx := context.Background()
	queries := db.New(connection)
	output := map[string]any{}
	events := []struct {
		name string
		at   time.Time
	}{
		{name: "one_second_before", at: time.Date(1969, 12, 31, 23, 59, 59, 0, time.UTC)},
		{name: "a_minute_and_a_half_second_before", at: time.Date(1969, 12, 31, 23, 58, 59, 500000000, time.UTC)},
		{name: "a_microsecond_before", at: time.Date(1969, 12, 31, 23, 59, 59, 999999000, time.UTC)},
		{name: "half_a_second_before", at: time.Date(1969, 12, 31, 23, 59, 59, 500000000, time.UTC)},
	}
	for index, event := range events {
		err := queries.InsertEvent(ctx, db.InsertEventParams{ID: uint64(index + 1), OccurredAt: event.at})
		switch {
		case err == nil:
			output[event.name] = "written"
		case strings.Contains(err.Error(), "less than a second before 1970"):
			output[event.name] = "refused"
		default:
			return fmt.Errorf("InsertEvent %s: %w", event.name, err)
		}
	}
	listed, err := queries.ListEvents(ctx)
	if err != nil {
		return fmt.Errorf("ListEvents: %w", err)
	}
	output["ListEvents"] = listed
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
