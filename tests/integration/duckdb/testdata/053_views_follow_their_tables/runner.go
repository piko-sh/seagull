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

const plants = `CREATE TABLE plants (id INTEGER PRIMARY KEY, name VARCHAR NOT NULL, stock INTEGER NOT NULL);
CREATE VIEW plant_list AS SELECT * FROM plants;
CREATE VIEW plant_names (plant_id, plant_name) AS SELECT * FROM plants;`

const heights = `ALTER TABLE plants ADD COLUMN height DOUBLE;
ALTER TABLE plants ALTER COLUMN stock TYPE BIGINT;`

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
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, migration := range []string{plants, heights} {
		if _, err := conn.ExecContext(ctx, migration); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	tall := 1.5
	output.record("insert_plants", func() (any, error) {
		if err := queries.InsertPlant(ctx, db.InsertPlantParams{ID: 1, Name: "fern", Stock: 5000000000, Height: &tall}); err != nil {
			return nil, err
		}
		return "inserted", queries.InsertPlant(ctx, db.InsertPlantParams{ID: 2, Name: "moss", Stock: 3, Height: nil})
	})
	output.record("plant_list", func() (any, error) { return queries.ListPlants(ctx) })
	output.record("plant_names", func() (any, error) { return queries.ListPlantNames(ctx) })
	output.record("renamed_plants", func() (any, error) { return queries.ListRenamedPlants(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
