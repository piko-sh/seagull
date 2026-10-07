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

	"github.com/cockroachdb/apd/v3"
	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	_, err = conn.ExecContext(ctx, `CREATE TABLE readings (
		id     INTEGER PRIMARY KEY,
		body   JSON NOT NULL,
		amount DECIMAL(8, 2) NOT NULL,
		span   INTERVAL NOT NULL,
		note   JSON,
		attrs  MAP(VARCHAR, INTEGER),
		point  STRUCT(x INTEGER, y INTEGER)
	)`)
	if err != nil {
		return err
	}

	queries := db.New(conn)
	amount, _, err := apd.NewFromString("12.50")
	if err != nil {
		return err
	}
	note := db.JSON(`{"by":"tester"}`)
	err = queries.AddReading(ctx, db.AddReadingParams{
		ID:     1,
		Body:   db.JSON(`{"kind":"reading","value":7}`),
		Amount: *amount,
		Span:   "01:30:00",
		Note:   note,
	})
	if err != nil {
		return fmt.Errorf("AddReading: %w", err)
	}
	_, err = conn.ExecContext(ctx, `UPDATE readings SET attrs = MAP {'depth': 3, 'width': 4}, point = {'x': 1, 'y': 2} WHERE id = 1`)
	if err != nil {
		return err
	}

	reading, err := queries.GetReading(ctx, 1)
	if err != nil {
		return fmt.Errorf("GetReading: %w", err)
	}
	threshold, _, err := apd.NewFromString("10.00")
	if err != nil {
		return err
	}
	over, err := queries.ReadingsOver(ctx, *threshold)
	if err != nil {
		return fmt.Errorf("ReadingsOver: %w", err)
	}

	var body map[string]any
	if err := json.Unmarshal(reading.Body, &body); err != nil {
		return err
	}
	result := map[string]any{
		"id":           reading.ID,
		"body":         body,
		"amount":       reading.Amount.String(),
		"span":         reading.Span,
		"note_present": reading.Note != nil,
		"attrs":        reading.Attrs,
		"point":        reading.Point,
		"over_count":   len(over),
		"over_amount":  over[0].Amount.String(),
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}
