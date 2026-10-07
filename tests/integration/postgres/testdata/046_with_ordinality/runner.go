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

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err = conn.ExecContext(ctx, "SET search_path TO "+os.Getenv("DATABASE_SCHEMA")); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx,
		`INSERT INTO playlists (id, tracks, ratings) VALUES (1, ARRAY['intro', 'verse', 'outro'], ARRAY[5, 3]), (2, ARRAY['solo'], NULL)`); err != nil {
		return err
	}
	queries := db.New(conn)
	output := map[string]any{}
	if output["tracks_in_order"], err = queries.TracksInOrder(ctx, 1); err != nil {
		return fmt.Errorf("TracksInOrder: %w", err)
	}
	if output["optional_ratings"], err = queries.OptionalRatings(ctx); err != nil {
		return fmt.Errorf("OptionalRatings: %w", err)
	}
	if output["numbered_series"], err = queries.NumberedSeries(ctx); err != nil {
		return fmt.Errorf("NumberedSeries: %w", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
