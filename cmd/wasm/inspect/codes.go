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
	"maps"
	"strings"

	"seagull.sh/seagull/cmd/wasm/internal/session"
)

const (
	// postgres names the dialect most reproductions are written for.
	postgres = "postgres"

	// mysql names the dialect of the reproductions that need a database without RETURNING.
	mysql = "mysql"

	// clickhouse names the dialect of the reproductions that need asynchronous mutations.
	clickhouse = "clickhouse"

	// sqlite names the dialect of the reproductions that need views that follow their
	// tables.
	sqlite = "sqlite"

	// reproductionQueries is the query file each reproduction replaces.
	reproductionQueries = "db/queries/example.sql"

	// reproductionSchemaPath is the second migration every PostgreSQL reproduction adds.
	reproductionSchemaPath = "db/migrations/0002_books.up.sql"

	// reproductionSchema adds a books table to the PostgreSQL starter's authors table.
	reproductionSchema = "CREATE TABLE books (\n" +
		"    id        BIGSERIAL PRIMARY KEY,\n" +
		"    author_id BIGINT NOT NULL REFERENCES authors (id),\n" +
		"    title     TEXT NOT NULL,\n" +
		"    pages     INT NOT NULL,\n" +
		"    shelf     INT GENERATED ALWAYS AS (pages / 100) STORED\n" +
		");\n"

	// directiveLineLimit is the most directive lines sgl reads from one query header.
	directiveLineLimit = 10_000
)

// unreproducible records why a diagnostic code has no small example project.
//
// The -codes output includes these explanations for the website.
var unreproducible = map[string]string{
	"Q030": "It reports an internal inconsistency in analysis that no project should reach. A custom function in " +
		"sgl.yaml that cannot be converted would raise it too, but sgl.yaml's own checks reject such a declaration " +
		"before analysis starts.",
	"Q045": "Every built-in dialect declares whether the functions it resolves read or modify data, so only a " +
		"third-party dialect can raise it, and the playground runs the built-in dialects.",
	"Q054": "It reports that analysis stopped at a nesting depth limit. Every built-in dialect's parser rejects a " +
		"statement nested that deeply first, with Q010, so no short project reaches it.",
	"Q066": "It reports that more diagnostics were found than the limit allows from one query or run, far more " +
		"than a short example holds.",
	"Q070": "It reports a panic in Seagull, a dialect or host-supplied code. The built-in dialects do not panic on " +
		"any short project, and the playground runs no host code.",
}

// codeView documents one diagnostic code, with an example where one is reproduced.
type codeView struct {
	// Example is the diagnostic as the playground presents it, or nil when no short project
	// reproduces the code.
	Example *session.DiagnosticView `json:"example"`

	// Dialect names the dialect the example was reproduced with, or is empty without an
	// example.
	Dialect string `json:"dialect"`

	// Unreproducible says why no short project raises the code, or is empty when one does.
	Unreproducible string `json:"unreproducible"`

	// Explanation is the code's documentation.
	session.ExplainView
}

// reproduction is a short project that raises one diagnostic code: a dialect's starter
// with its query file replaced and, optionally, other files added. A PostgreSQL
// reproduction also adds reproductionSchema.
type reproduction struct {
	// files holds the files added to or replaced in the starter.
	files map[string]string

	// dialect names the dialect whose starter the project builds on.
	dialect string

	// queries is the content of the query file.
	queries string
}

// reproductions maps diagnostic codes to the projects that raise them.
var reproductions = map[string]reproduction{
	"Q001": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one)\nSELECT id, nmae FROM authors WHERE id = $1;\n"},
	"Q002": {files: nil, dialect: postgres, queries: "-- sgl.query(ListBooks, many)\nSELECT id, title FROM books JOIN authors ON authors.id = books.author_id;\n"},
	"Q003": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM author WHERE id = $1;\n"},
	"Q005": {files: nil, dialect: postgres, queries: "-- sgl.query(ListBooks, many)\nSELECT id, lenth(title) AS letters FROM books;\n"},
	"Q006": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one)\nSELECT id FROM authors WHERE id = $1;\n\n" +
		"-- sgl.query(GetAuthor, one)\nSELECT name FROM authors WHERE id = $1;\n"},
	"Q007": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one, readonly true)\nSELECT id FROM authors WHERE id = $1;\n"},
	"Q008": {files: nil, dialect: postgres, queries: "SELECT id, name FROM authors WHERE id = $1;\n"},
	"Q009": {files: nil, dialect: postgres, queries: "-- sgl.query(AddAuthor, one)\nINSERT INTO authors (name) VALUES ($1);\n"},
	"Q010": {
		files:   map[string]string{"db/migrations/0003_notes.up.sql": "CREATE TABLE notes (id BIGINT,, body TEXT);\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q011": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthors, many)\n-- sgl.sortable(order_by, [name, created_at])\nSELECT id, name FROM authors\n"},
	"Q012": {files: nil, dialect: postgres, queries: "-- sgl.query(TouchAuthor, exec)\nUPDATE authors SET bio = NULL WHERE id = $1;\nDELETE FROM authors WHERE id = $1;\n"},
	"Q013": {files: nil, dialect: postgres, queries: "-- sgl.query(AddBook, exec)\nINSERT INTO books (author_id, title, pages, shelf) VALUES ($1, $2, $3, $4);\n"},
	"Q014": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthors, many, group_by: authors.bio)\nSELECT id, name FROM authors;\n"},
	"Q015": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthorBooks, many, group_by: authors.id)\n" +
		"SELECT authors.id, books.title FROM authors JOIN books ON books.author_id = authors.id;\n"},
	"Q016": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthorBooks, one, group_by: authors.id)\n-- sgl.embed(books, from: b)\n" +
		"SELECT authors.id, b.id, b.title FROM authors JOIN books b ON b.author_id = authors.id WHERE authors.id = $1;\n"},
	"Q017": {files: nil, dialect: postgres, queries: "-- sgl.query(AddBooks, copyfrom)\n-- $1 as sgl.param(ids, kind: slice)\n" +
		"INSERT INTO books (author_id, title, pages) VALUES ($1, $2, $3);\n"},
	"Q018": {files: nil, dialect: postgres, queries: "-- sgl.query(SearchAuthors, many, dynamic: runtime)\n-- $1 as sgl.param(ids, kind: slice)\n" +
		"SELECT id FROM authors WHERE id = ANY($1)\n"},
	"Q020": {files: nil, dialect: postgres, queries: "-- sgl.query(Names, many)\nSELECT id FROM authors UNION SELECT id, title FROM books;\n"},
	"Q022": {files: nil, dialect: postgres, queries: "-- sgl.query(SearchAuthors, many, dynamic: runtime)\nSELECT id, name FROM authors WHERE bio IS NOT NULL\n"},
	"Q023": {files: nil, dialect: postgres, queries: "-- sgl.query(SearchShelves, many, dynamic: runtime)\nSELECT author_id, count(*) AS books FROM books GROUP BY author_id\n"},
	"Q024": {files: nil, dialect: postgres, queries: "-- sgl.query(PurgeAuthors, many, dynamic: runtime)\nDELETE FROM authors RETURNING id, name\n"},
	"Q026": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one)\n-- sgl.colum(name, type: text)\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q027": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one, optinal: true)\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q028": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthors, mnay)\nSELECT id, name FROM authors;\n"},
	"Q029": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one, command: many)\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q031": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor)\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q032": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q033": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one)\n-- $1 as sql.param(author_id)\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q034": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthors, many)\n-- sgl.sortable(order_by, [name, id)\nSELECT id, name FROM authors\n"},
	"Q035": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one)\n-- sgl.column(name, type: \"text)\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q036": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one)\n-- sgl.column(nickname, type: text)\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q037": {
		files: map[string]string{"sgl.yaml": "version: 1\ndatabases:\n  - name: app\n    dialect: postgres\n    go:\n      types:\n        overrides:\n" +
			"          - column: authors.nickname\n            go: string\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q038": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthors, many)\n-- $1 as sgl.param(page_size, optional: true, default: 10)\n" +
		"SELECT id FROM authors LIMIT $1;\n"},
	"Q039": {
		files:   nil,
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\n" + strings.Repeat("-- $1 as sgl.param(id)\n", directiveLineLimit) +
			"SELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q040": {files: nil, dialect: clickhouse, queries: "-- sgl.query(ClearBio, exec)\nALTER TABLE authors UPDATE bio = NULL WHERE id = {id:UInt64};\n"},
	"Q041": {files: nil, dialect: postgres, queries: "-- sgl.query(PurgeAuthors, asyncexec)\nDELETE FROM authors;\n"},
	"Q042": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one)\n-- $2 as sgl.param(unused)\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q043": {
		files: map[string]string{"db/migrations/0003_index.up.sql": "CREATE INDEX CONCURRENTLY authors_name ON authors (name);\n" +
			"CREATE TABLE notes (id BIGINT);\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q044": {
		files: map[string]string{"db/migrations/0003_gulls.up.sql": "CREATE TABLE gulls (\n    id   BIGSERIAL PRIMARY KEY,\n    name TEXT NOT NULL\n);\n\n" +
			"-- sgl.query(ListGulls, many)\nSELECT id, name FROM gulls;\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q046": {files: nil, dialect: postgres, queries: "-- sgl.query(AddAuthor, one)\nINSERT INTO authors (name) VALUES ($1) ON CONFLICT DO NOTHING RETURNING id;\n"},
	"Q047": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthors, many, optional: true)\nSELECT id, name FROM authors;\n"},
	"Q048": {
		files:   map[string]string{"db/migrations/notes.sql": "CREATE TABLE notes (id BIGINT);\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q049": {
		files: map[string]string{"db/migrations/0003_gulls.up.sql": "CREATE TABLE gulls (\n    id   UUID PRIMARY KEY,\n    name TEXT NOT NULL\n);\n" +
			"-- sgl.column(gulls.id, go_type: \"example.com/gulls.identifier\")\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetGull, one)\nSELECT id, name FROM gulls WHERE id = $1;\n",
	},
	"Q050": {
		files:   map[string]string{"sgl.yaml": "version: 1\ndirectives:\n  require_canonical: true\ndatabases:\n  - name: app\n    dialect: postgres\n"},
		dialect: postgres,
		queries: "-- seagull.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q051": {files: nil, dialect: postgres, queries: "-- sgl.query(GetAuthor, one)\n-- piko.column(name, type: text)\nSELECT id, name FROM authors WHERE id = $1;\n"},
	"Q052": {files: nil, dialect: mysql, queries: "-- sgl.query(AddAuthor, one)\nINSERT INTO authors (name) VALUES (?) RETURNING id;\n"},
	"Q053": {
		files:   map[string]string{"db/migrations/0002_shelves.up.sql": "CREATE TABLE shelves (\n    id BIGSERIAL PRIMARY KEY\n);\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q055": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthors, many)\n-- $1 as sgl.param(ids, type: int8, kind: slice)\n" +
		"SELECT id, name FROM authors WHERE id = ANY($1);\n"},
	"Q056": {files: nil, dialect: clickhouse, queries: "-- sgl.query(FindAuthors, many)\n-- {name:String} as sgl.param(name, optional: true)\n" +
		"SELECT id, name FROM authors WHERE name = {name:String};\n"},
	"Q057": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthors, many)\n-- sgl.sortable(order_by, [name, id])\n" +
		"SELECT id, name FROM authors ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END;\n"},
	"Q058": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthors, many)\nSET LOCAL statement_timeout = $1;\nSELECT id, name FROM authors;\n"},
	"Q059": {files: nil, dialect: clickhouse, queries: "-- sgl.query(ClearBio, execrows)\nALTER TABLE authors UPDATE bio = NULL WHERE id = {id:UInt64};\n"},
	"Q060": {files: nil, dialect: postgres, queries: "-- sgl.query(AddAuthors, batch)\n" +
		"INSERT INTO authors (name) VALUES ($1) ON CONFLICT (id) DO UPDATE SET bio = $2;\n"},
	"Q061": {
		files: map[string]string{"sgl.yaml": "version: 1\ndatabases:\n  - name: app\n    dialect: postgres\n    go:\n      types:\n        overrides:\n" +
			"          - sql: geography\n            go: example.com/geo.Point\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q062": {files: nil, dialect: postgres, queries: "-- sgl.query(CountAuthors, one)\nSELECT name, count(*) AS total FROM authors;\n"},
	"Q004": {files: nil, dialect: postgres, queries: "-- sgl.query(RenameAuthor, exec)\nUPDATE authors SET name = $1 WHERE id = $1;\n"},
	"Q064": {files: nil, dialect: postgres, queries: "-- sgl.query(AddAuthor, exec)\nINSERT INTO authors (name) VALUES ($1, $2);\n"},
	"Q065": {files: nil, dialect: postgres, queries: "-- sgl.query(ListAuthorIDs, many, model: authors)\nSELECT id FROM authors;\n"},
	"Q063": {files: nil, dialect: mysql, queries: "-- sgl.query(ListTitles, many)\nSELECT 'Author: ' || name AS title FROM authors;\n"},
	"Q067": {
		files:   map[string]string{"db/migrations/0003_rename.up.sql": "ALTER TABLE books RENAME TO authors;\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q068": {
		files:   map[string]string{"db/migrations/0003_rename.up.sql": "ALTER TABLE shelves RENAME TO racks;\n"},
		dialect: postgres,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = $1;\n",
	},
	"Q069": {
		files:   map[string]string{"db/migrations/0002_names.up.sql": "CREATE VIEW author_names AS TABLE authors;\n"},
		dialect: mysql,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = ?;\n",
	},
	"Q071": {
		files: map[string]string{"db/migrations/0002_views.up.sql": "CREATE VIEW a AS SELECT 1 AS seed;\n" +
			"CREATE VIEW b AS SELECT *, 2 AS more FROM a;\nDROP VIEW a;\nCREATE VIEW a AS SELECT *, 3 AS again FROM b;\n"},
		dialect: sqlite,
		queries: "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = ?1;\n",
	},
}

// codeViews documents every diagnostic code, reproducing an example for each code the
// reproductions cover and giving the reason for each code they cannot.
//
// Returns []codeView which holds one entry per code, in ascending order.
func codeViews(ctx context.Context) []codeView {
	codes := session.Codes()
	views := make([]codeView, 0, len(codes))
	for _, code := range codes {
		example, dialect := reproduce(ctx, code.Code)
		views = append(views, codeView{Example: example, Dialect: dialect, Unreproducible: unreproducible[code.Code], ExplainView: code})
	}
	return views
}

// reproduce runs a code's reproduction and returns the first diagnostic with that code.
//
// Takes code (string) which is the diagnostic code.
//
// Returns *session.DiagnosticView which is the diagnostic, or nil when the code has no
// reproduction or the reproduction no longer raises it.
// Returns string which names the dialect the run used, or is empty without a diagnostic.
func reproduce(ctx context.Context, code string) (*session.DiagnosticView, string) {
	project, found := reproductions[code]
	if !found {
		return nil, ""
	}
	files, err := projectFiles(project)
	if err != nil {
		return nil, ""
	}
	response := session.Run(ctx, session.Request{Files: files, Override: session.Override{}, V: session.ProtocolVersion})
	if response.Config == nil {
		return nil, ""
	}
	for index := range response.Diagnostics {
		if response.Diagnostics[index].Code == code {
			return &response.Diagnostics[index], response.Config.Dialect
		}
	}
	return nil, ""
}

// projectFiles lays a reproduction over its dialect's starter.
//
// Takes project (reproduction) which is laid over the starter.
//
// Returns map[string]string which maps each of the project's paths to its content.
// Returns error when the reproduction's dialect has no starter.
func projectFiles(project reproduction) (map[string]string, error) {
	files, err := session.Starter(project.dialect)
	if err != nil {
		return nil, err
	}
	if project.dialect == postgres {
		files[reproductionSchemaPath] = reproductionSchema
	}
	files[reproductionQueries] = project.queries
	maps.Copy(files, project.files)
	return files, nil
}
