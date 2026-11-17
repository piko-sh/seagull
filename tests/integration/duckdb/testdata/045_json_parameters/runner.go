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

	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

const schema = `CREATE TABLE documents (
    id INTEGER PRIMARY KEY,
    data JSON
);

CREATE TABLE composites (
    id INTEGER PRIMARY KEY,
    documents JSON[] NOT NULL,
    pair STRUCT(doc JSON, n INTEGER) NOT NULL
);
`

const document = `{"b": [1, 2.50], "a": "<é & 😀>"}`

const seed = `INSERT INTO documents (id, data) VALUES (1, '{"b": [1, 2.50], "a": "<é & 😀>"}');
INSERT INTO composites (id, documents, pair) VALUES (1, ['{"b": [1, 2.50], "a": "<é & 😀>"}'::JSON], {'doc': '{"b": [1, 2.50], "a": "<é & 😀>"}'::JSON, 'n': 1})`

type cells map[string]string

func (c cells) record(name string, write func() error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c[name] = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	if err := write(); err != nil {
		c[name] = err.Error()
		return
	}
	c[name] = "ok"
}

func (c cells) read(name string, read func() (db.JSON, error)) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c[name] = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	got, err := read()
	switch {
	case err != nil:
		c[name] = err.Error()
	case got == nil:
		c[name] = "null"
	case string(got) == document:
		c[name] = "exact"
	default:
		c[name] = "read " + string(got)
	}
}

func listed[T any](found map[string]any, c cells, name string, list func() (T, error)) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c[name] = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	rows, err := list()
	if err != nil {
		c[name] = err.Error()
		return
	}
	c[name] = "ok"
	found[name] = rows
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
	if _, err = conn.ExecContext(ctx, schema); err != nil {
		return err
	}
	queries := db.New(conn)
	results := cells{}
	found := map[string]any{}
	written := db.JSON(document)

	if _, err := conn.ExecContext(ctx, seed); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	results.record("exec", func() error { return queries.RecordDocument(ctx, db.RecordDocumentParams{ID: 11, Data: written}) })
	results.record("batch", func() error {
		return queries.RecordDocuments(ctx, []db.RecordDocumentsParams{{ID: 12, Data: written}, {ID: 13, Data: nil}})
	})
	results.record("exec null", func() error { return queries.RecordDocument(ctx, db.RecordDocumentParams{ID: 21, Data: nil}) })

	results.read("read 1", func() (db.JSON, error) {
		row, err := queries.ReadDocument(ctx, 1)
		return row.Data, err
	})
	results.read("read 11", func() (db.JSON, error) {
		row, err := queries.ReadDocument(ctx, 11)
		return row.Data, err
	})
	results.read("read 12", func() (db.JSON, error) {
		row, err := queries.ReadDocument(ctx, 12)
		return row.Data, err
	})
	results.read("read 21", func() (db.JSON, error) {
		row, err := queries.ReadDocument(ctx, 21)
		return row.Data, err
	})
	listed(found, results, "storage", func() ([]db.ListStorageRow, error) { return queries.ListStorage(ctx) })
	listed(found, results, "literal", func() ([]db.FindByLiteralRow, error) { return queries.FindByLiteral(ctx) })
	listed(found, results, "parameter", func() ([]db.FindByParameterRow, error) { return queries.FindByParameter(ctx, written) })

	results.record("composite exec", func() error {
		return queries.RecordComposite(ctx, db.RecordCompositeParams{ID: 11, Documents: []db.JSON{written}, Pair: db.CompositesPair{Doc: written, N: new(int32(11))}})
	})
	results.read("read composite 1 element", func() (db.JSON, error) {
		row, err := queries.ReadComposite(ctx, 1)
		if err != nil {
			return nil, err
		}
		return row.Documents[0], nil
	})
	results.read("read composite 1 field", func() (db.JSON, error) {
		row, err := queries.ReadComposite(ctx, 1)
		if err != nil {
			return nil, err
		}
		return row.Pair.Doc, nil
	})
	results.read("read composite 11 element", func() (db.JSON, error) {
		row, err := queries.ReadComposite(ctx, 11)
		if err != nil {
			return nil, err
		}
		return row.Documents[0], nil
	})
	results.read("read composite 11 field", func() (db.JSON, error) {
		row, err := queries.ReadComposite(ctx, 11)
		if err != nil {
			return nil, err
		}
		return row.Pair.Doc, nil
	})
	listed(found, results, "composite storage", func() ([]db.ListCompositeStorageRow, error) { return queries.ListCompositeStorage(ctx) })
	listed(found, results, "composite literal", func() ([]db.FindCompositeByLiteralRow, error) { return queries.FindCompositeByLiteral(ctx) })

	return json.NewEncoder(os.Stdout).Encode(map[string]any{"cells": results, "found": found})
}
