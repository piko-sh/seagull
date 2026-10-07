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

const schema = `CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE books (id INTEGER PRIMARY KEY, author_id INTEGER NOT NULL REFERENCES authors (id), title TEXT NOT NULL, price INTEGER);
INSERT INTO authors VALUES (1, 'Agatha'), (2, 'Dorothy');
INSERT INTO books VALUES (1, 1, 'The Murder at the Vicarage', 12), (2, 1, 'Nemesis', NULL), (3, 2, 'Gaudy Night', 9);`

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func main() {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	output := results{}
	output.record("author_titles", func() (any, error) { return queries.AuthorTitles(ctx) })
	output.record("nested_with", func() (any, error) { return queries.NestedWith(ctx) })
	output.record("star_of_an_earlier_cte", func() (any, error) { return queries.StarOfAnEarlierCTE(ctx) })
	output.record("joined_earlier_cte", func() (any, error) { return queries.JoinedEarlierCTE(ctx) })
	output.record("cte_inside_a_derived_union", func() (any, error) { return queries.CTEInsideADerivedUnion(ctx) })
	output.record("derived_table_in_a_later_arm", func() (any, error) { return queries.DerivedTableInALaterArm(ctx) })
	output.record("renamed_star_cte", func() (any, error) { return queries.RenamedStarCTE(ctx) })
	output.record("authors_with_first_title", func() (any, error) { return queries.AuthorsWithFirstTitle(ctx) })
	output.record("authors_above_count", func() (any, error) { return queries.AuthorsAboveCount(ctx, 1) })
	output.record("null_in_the_first_branch", func() (any, error) { return queries.NullInTheFirstBranch(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
