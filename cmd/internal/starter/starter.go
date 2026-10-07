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

// Package starter holds the starter project `sgl init` writes: sgl.yaml, a first
// migration, an example query file and a seed file, with SQL for each dialect family. The
// in-browser playground opens on the same files, so the two never drift apart.
package starter

import (
	"errors"
	"fmt"
	"strings"

	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

const (
	// configTemplate is the sgl.yaml of the starter project; %s is the dialect.
	configTemplate = `# sgl.yaml configures sgl. Every setting: https://seagull.sh/docs/reference/configuration
version: 1

databases:
  - name: app
    dialect: %s
    migrations: db/migrations
    queries: db/queries
    seeds: db/seeds
    go:
      package: store
      out: internal/store
      driver: database/sql
`

	// seedAuthor is the seed row for dialects that number the authors themselves.
	seedAuthor = "INSERT INTO authors (name, bio) VALUES ('Ada Lovelace', 'Wrote the first program.');"

	// seedAuthorWithID is the seed row for dialects without an automatic key.
	seedAuthorWithID = "INSERT INTO authors (id, name, bio) VALUES (1, 'Ada Lovelace', 'Wrote the first program.');"

	// lookupQuestion reads one author with a question-mark placeholder.
	lookupQuestion = "SELECT id, name, bio FROM authors WHERE id = ?;"

	// insertQuestion adds an author with question-mark placeholders.
	insertQuestion = "INSERT INTO authors (name, bio) VALUES (?, ?);"
)

// ErrNoStarter is returned by Files for a dialect family with no starter SQL.
var ErrNoStarter = errors.New("no starter files")

// File is one file of the starter project.
type File struct {
	// Path is slash-separated and relative to the project's root.
	Path string

	// Content is the file's content.
	Content string
}

// familySQL holds the starter SQL for one dialect family.
type familySQL struct {
	// table is the column list and suffix of the authors table.
	table string

	// lookup is the query that reads one author.
	lookup string

	// insert is the query that adds an author.
	insert string

	// seed is the seed data.
	seed string
}

// families holds the starter SQL for each dialect family.
var families = map[string]familySQL{
	dialect_engine_postgres.Family: {
		table:  "    id   BIGSERIAL PRIMARY KEY,\n    name TEXT NOT NULL,\n    bio  TEXT\n)",
		lookup: "SELECT id, name, bio FROM authors WHERE id = $1;",
		insert: "INSERT INTO authors (name, bio) VALUES ($1, $2);",
		seed:   seedAuthor,
	},
	dialect_engine_mysql.Family: {
		table:  "    id   BIGINT AUTO_INCREMENT PRIMARY KEY,\n    name VARCHAR(255) NOT NULL,\n    bio  TEXT\n)",
		lookup: lookupQuestion,
		insert: insertQuestion,
		seed:   seedAuthor,
	},
	dialect_engine_sqlite.Family: {
		table:  "    id   INTEGER PRIMARY KEY,\n    name TEXT NOT NULL,\n    bio  TEXT\n)",
		lookup: lookupQuestion,
		insert: insertQuestion,
		seed:   seedAuthor,
	},
	dialect_engine_duckdb.Family: {
		table:  "    id   BIGINT PRIMARY KEY,\n    name VARCHAR NOT NULL,\n    bio  VARCHAR\n)",
		lookup: "SELECT id, name, bio FROM authors WHERE id = $1;",
		insert: "INSERT INTO authors (id, name, bio) VALUES ($1, $2, $3);",
		seed:   seedAuthorWithID,
	},
	dialect_engine_clickhouse.Family: {
		table:  "    id   UInt64,\n    name String,\n    bio  Nullable(String)\n) ENGINE = MergeTree() ORDER BY id",
		lookup: "SELECT id, name, bio FROM authors WHERE id = {id:UInt64};",
		insert: "INSERT INTO authors (id, name, bio) VALUES ({id:UInt64}, {name:String}, {bio:Nullable(String)});",
		seed:   seedAuthorWithID,
	},
}

// Files builds the starter project for a dialect.
//
// Takes dialectName (string) which is written to sgl.yaml as the dialect.
// Takes family (string) which is the dialect's family, such as
// dialect_engine_postgres.Family; it chooses the SQL.
//
// Returns []File which holds sgl.yaml, the up and down migrations, the query file and the
// seed file, in the order sgl init writes them.
// Returns error which wraps ErrNoStarter when the family has no starter SQL.
func Files(dialectName, family string) ([]File, error) {
	chosen, found := families[family]
	if !found {
		return nil, fmt.Errorf("%w for dialect %s of family %q", ErrNoStarter, dialectName, family)
	}
	queries := strings.Join([]string{
		"-- sgl.query(GetAuthor, one)",
		chosen.lookup,
		"",
		"-- sgl.query(ListAuthors, many)",
		"SELECT id, name, bio FROM authors ORDER BY name;",
		"",
		"-- sgl.query(CreateAuthor, exec)",
		chosen.insert,
		"",
	}, "\n")
	return []File{
		{Path: "sgl.yaml", Content: fmt.Sprintf(configTemplate, dialectName)},
		{Path: "db/migrations/0001_init.up.sql", Content: "CREATE TABLE authors (\n" + chosen.table + ";\n"},
		{Path: "db/migrations/0001_init.down.sql", Content: "DROP TABLE authors;\n"},
		{Path: "db/queries/example.sql", Content: queries},
		{Path: "db/seeds/0001_authors.sql", Content: chosen.seed + "\n"},
	}, nil
}
