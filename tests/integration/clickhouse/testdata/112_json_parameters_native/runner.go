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
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

const document = `{"b": [1, 2.50], "a": "<é & 😀>"}`

const seed = `INSERT INTO documents (id, data) VALUES (1, '{"b": [1, 2.50], "a": "<é & 😀>"}')`

const compositeSeed = `INSERT INTO composites (id, documents, pair) VALUES (1, ['{"b": [1, 2.50], "a": "<é & 😀>"}'], ('{"b": [1, 2.50], "a": "<é & 😀>"}', 1))`

const richDocument = `{"date": "2024-01-02", "path": "a/b", "big": 18446744073709551615, "nested": {"z": 1}, "nothing": null, "float": 1e21}`

var readSeeds = []string{
	`INSERT INTO owners (id, name) VALUES (1, 'one'), (11, 'eleven')`,
	`INSERT INTO optional_documents (id, data) VALUES (1, NULL), (2, '{"b": [1, 2.50], "a": "<é & 😀>"}')`,
	`INSERT INTO nested_documents (id, keyed, optional_keyed, listed, optional_list, pairs, unnamed, listed_unnamed, keyed_unnamed) ` +
		`VALUES (1, map('k', '{"b": [1, 2.50], "a": "<é & 😀>"}'), map('k', '{"b": [1, 2.50], "a": "<é & 😀>"}', 'n', NULL), ` +
		`map('k', ['{"b": [1, 2.50], "a": "<é & 😀>"}']), ['{"b": [1, 2.50], "a": "<é & 😀>"}', NULL], ` +
		`map('k', ('{"b": [1, 2.50], "a": "<é & 😀>"}', 1)), ('{"b": [1, 2.50], "a": "<é & 😀>"}', 2), ` +
		`[('{"b": [1, 2.50], "a": "<é & 😀>"}', 3)], map('k', ('{"b": [1, 2.50], "a": "<é & 😀>"}', 4)))`,
	`INSERT INTO rich_documents (id, data) VALUES (1, '` + richDocument + `')`,
}

type cells map[string]string

type value struct {
	read any
	name string
}

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

func (c cells) readValues(name string, read func() ([]value, error)) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c[name] = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	values, err := read()
	if err != nil {
		c[name] = err.Error()
		return
	}
	c[name] = strconv.Itoa(len(values)) + " read"
	for _, item := range values {
		c[name+" "+item.name] = describe(item.read)
	}
}

func describe(read any) string {
	switch typed := read.(type) {
	case db.JSON:
		switch {
		case typed == nil:
			return "null"
		case string(typed) == document:
			return "exact"
		default:
			return "read " + string(typed)
		}
	case string:
		return "text " + typed
	case nil:
		return "nil"
	default:
		return fmt.Sprintf("a %T", read)
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
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		return err
	}
	defer conn.Close()
	queries := db.New(conn)
	results := cells{}
	found := map[string]any{}
	written := db.JSON(document)

	if err := conn.Exec(ctx, seed); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	if err := conn.Exec(ctx, compositeSeed); err != nil {
		return fmt.Errorf("composite seed: %w", err)
	}
	results.record("exec", func() error { return queries.RecordDocument(ctx, db.RecordDocumentParams{ID: 11, Data: written}) })
	results.record("batch", func() error {
		return queries.RecordDocuments(ctx, []db.RecordDocumentsParams{{ID: 12, Data: written}, {ID: 13, Data: written}})
	})
	results.record("copyfrom", func() error {
		return queries.CopyDocuments(ctx, []db.CopyDocumentsParams{{ID: 14, Data: written}, {ID: 15, Data: written}})
	})
	results.record("batch rows", func() error {
		return queries.RecordDocumentRows(ctx, []db.RecordDocumentRowsParams{{ID: 16, Data: written}})
	})

	listed(found, results, "storage", func() ([]db.ListStorageRow, error) { return queries.ListStorage(ctx) })
	listed(found, results, "literal", func() ([]db.FindByLiteralRow, error) { return queries.FindByLiteral(ctx) })
	listed(found, results, "parameter", func() ([]db.FindByParameterRow, error) { return queries.FindByParameter(ctx, written) })

	results.record("composite exec", func() error {
		return queries.RecordComposite(ctx, db.RecordCompositeParams{ID: 11, Documents: []db.JSON{written}, Pair: db.CompositesPair{Doc: written, N: 11}})
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

	for _, statement := range readSeeds {
		if err := conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("read seed: %w", err)
		}
	}
	readDocuments(ctx, queries, results)
	readShapes(ctx, queries, results)
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"cells": results, "found": found})
}

func readDocuments(ctx context.Context, queries *db.Queries, results cells) {
	for _, id := range []int32{1, 11, 16} {
		results.readValues("one "+strconv.Itoa(int(id)), func() ([]value, error) {
			row, err := queries.ReadDocument(ctx, id)
			return []value{{name: "data", read: row.Data}}, err
		})
	}
	results.readValues("stream", func() ([]value, error) {
		var values []value
		for row, err := range queries.StreamDocuments(ctx, 13) {
			if err != nil {
				return nil, err
			}
			values = append(values, value{name: strconv.Itoa(int(row.ID)), read: row.Data})
		}
		return values, nil
	})
	results.readValues("union", func() ([]value, error) {
		rows, err := queries.ReadDocumentUnion(ctx, db.ReadDocumentUnionParams{ID: 11, Other: 1})
		values := make([]value, 0, len(rows))
		for _, row := range rows {
			values = append(values, value{name: strconv.Itoa(int(row.ID)), read: row.Data})
		}
		return values, err
	})
	results.readValues("embed", func() ([]value, error) {
		rows, err := queries.ListOwnerDocuments(ctx)
		values := make([]value, 0, len(rows))
		for _, row := range rows {
			values = append(values, value{name: row.Name, read: row.Documents.Data})
		}
		return values, err
	})
	results.readValues("builder", func() ([]value, error) {
		rows, err := queries.SearchDocuments().Where("id", "=", int32(11)).All(ctx)
		values := make([]value, 0, len(rows))
		for _, row := range rows {
			values = append(values, value{name: strconv.Itoa(int(row.ID)), read: row.Data})
		}
		return values, err
	})
	for _, id := range []int32{1, 2} {
		results.readValues("optional "+strconv.Itoa(int(id)), func() ([]value, error) {
			row, err := queries.ReadOptionalDocument(ctx, id)
			return []value{{name: "data", read: row.Data}}, err
		})
	}
}

func readShapes(ctx context.Context, queries *db.Queries, results cells) {
	results.readValues("nested", func() ([]value, error) {
		row, err := queries.ReadNested(ctx, 1)
		if err != nil {
			return nil, err
		}
		values := []value{
			{name: "keyed", read: row.Keyed["k"]},
			{name: "optional keyed", read: row.OptionalKeyed["k"]},
			{name: "optional keyed null", read: row.OptionalKeyed["n"]},
			{name: "pair", read: row.Pairs["k"].Doc},
		}
		for index, document := range row.Listed["k"] {
			values = append(values, value{name: "listed " + strconv.Itoa(index), read: document})
		}
		for index, document := range row.OptionalList {
			values = append(values, value{name: "optional list " + strconv.Itoa(index), read: document})
		}
		for index, element := range row.Unnamed {
			values = append(values, value{name: "unnamed " + strconv.Itoa(index), read: element})
		}
		for index, element := range row.ListedUnnamed[0] {
			values = append(values, value{name: "listed unnamed " + strconv.Itoa(index), read: element})
		}
		for index, element := range row.KeyedUnnamed["k"] {
			values = append(values, value{name: "keyed unnamed " + strconv.Itoa(index), read: element})
		}
		return values, nil
	})
	results.readValues("composite union", func() ([]value, error) {
		rows, err := queries.ReadCompositeUnion(ctx, db.ReadCompositeUnionParams{ID: 11, Other: 1})
		values := make([]value, 0, len(rows))
		for _, row := range rows {
			for index, document := range row.Documents {
				values = append(values, value{name: strconv.Itoa(int(row.ID)) + " element " + strconv.Itoa(index), read: document})
			}
			values = append(values, value{name: strconv.Itoa(int(row.ID)) + " field", read: row.Pair.Doc})
		}
		return values, err
	})
	results.readValues("rich", func() ([]value, error) {
		row, err := queries.ReadRichDocument(ctx, 1)
		if err != nil {
			return nil, err
		}
		values := []value{{name: "data", read: row.Data}, {name: "keyed", read: row.Keyed["k"]}, {name: "stored", read: row.Stored}}
		for index, document := range row.Listed {
			values = append(values, value{name: "listed " + strconv.Itoa(index), read: document})
		}
		return values, nil
	})
}
