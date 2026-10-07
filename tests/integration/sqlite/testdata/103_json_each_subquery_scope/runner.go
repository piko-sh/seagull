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

func main() {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fail(err)
	}
	defer conn.Close()

	ctx := context.Background()

	if _, err = conn.ExecContext(ctx, `CREATE TABLE customers (id INTEGER PRIMARY KEY, company_contacts TEXT NOT NULL)`); err != nil {
		fail(err)
	}

	if _, err = conn.ExecContext(ctx, `INSERT INTO customers (id, company_contacts) VALUES
		(1, '[{"uuid":"aaa","name":"Katie Arden"},{"uuid":"bbb","name":"Mia Richardson"}]'),
		(2, '[{"uuid":"ccc","name":"Dave Smith"}]')`); err != nil {
		fail(err)
	}

	queries := db.New(conn)

	existsMatches, err := queries.SearchContactsExists(ctx, "Mia")
	if err != nil {
		fail(err)
	}

	scalarMatches, err := queries.SearchContactsScalar(ctx, "bbb")
	if err != nil {
		fail(err)
	}

	result := map[string]any{
		"exists_search_mia": existsMatches,
		"scalar_search_bbb": scalarMatches,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
