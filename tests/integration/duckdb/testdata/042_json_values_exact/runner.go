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

const schema = `CREATE TABLE documents (
    id     INTEGER PRIMARY KEY,
    body   JSON NOT NULL,
    note   JSON,
    amount DECIMAL(8, 2) NOT NULL,
    span   INTERVAL NOT NULL,
    bodies JSON[]
);
INSERT INTO documents VALUES
    (1, '"a string"', NULL, 1.50, INTERVAL 90 MINUTE, ['"listed"'::JSON, '12345678901234567890'::JSON, NULL]),
    (2, '12345678901234567890', 'true', -0.05, INTERVAL 14 MONTH, NULL),
    (3, '{"nested": {"n": 12345678901234567890, "f": 1.10}, "list": [1, "two", null]}', 'null', 0, INTERVAL 0 DAY, []);
`

type described struct {
	Text  *string `json:"text"`
	Valid bool    `json:"valid"`
}

func describe(document db.JSON) described {
	if document == nil {
		return described{Text: nil, Valid: false}
	}
	text := string(document)
	return described{Text: &text, Valid: json.Valid(document)}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err = conn.ExecContext(ctx, schema); err != nil {
		return err
	}
	queries := db.New(conn)
	var amount apd.Decimal
	if _, _, err = amount.SetString("7.25"); err != nil {
		return err
	}
	if err = queries.InsertDocument(ctx, db.InsertDocumentParams{
		ID:     4,
		Body:   db.JSON(`{"written": 98765432109876543210, "s": "x"}`),
		Note:   db.JSON(`"quoted"`),
		Amount: amount,
		Span:   "2 days",
	}); err != nil {
		return fmt.Errorf("InsertDocument: %w", err)
	}
	documents, err := queries.ListDocuments(ctx)
	if err != nil {
		return fmt.Errorf("ListDocuments: %w", err)
	}
	listed := make([]map[string]any, 0, len(documents))
	for _, document := range documents {
		bodies := make([]described, 0, len(document.Bodies))
		for _, body := range document.Bodies {
			bodies = append(bodies, describe(body))
		}
		encoded, err := json.Marshal(document)
		if err != nil {
			return fmt.Errorf("marshalling document %d: %w", document.ID, err)
		}
		listed = append(listed, map[string]any{
			"id":      document.ID,
			"body":    describe(document.Body),
			"note":    describe(document.Note),
			"bodies":  bodies,
			"encoded": string(encoded),
		})
	}
	firstTwo, err := queries.FirstTwo(ctx)
	if err != nil {
		return fmt.Errorf("FirstTwo: %w", err)
	}
	compound := make([]map[string]any, 0, len(firstTwo))
	for _, row := range firstTwo {
		compound = append(compound, map[string]any{
			"body":   describe(row.Body),
			"note":   describe(row.Note),
			"amount": row.Amount.String(),
			"span":   row.Span,
		})
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"documents": listed, "compound": compound})
}
