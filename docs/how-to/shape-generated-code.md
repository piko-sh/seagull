---
title: Tag, document and mock generated code
description: Generate an interface of the query methods, struct tags, JSON tag styles,
  empty slices for no rows, rows read into a table's model or a shared struct, and doc
  comments from the SQL.
section: How-to guides
subsection: Results
order: 490
---

# Tag, document and mock generated code

Configure the generated API to fit its callers: expose an interface for tests, add field tags, share row types or carry SQL comments into Go. The optional settings below are off by default.

## Depend on an interface of the query methods

Turn the interface on beneath the selected database's `go` section in `sgl.yaml`:

```yaml
version: 1
databases:
  - name: app
    dialect: sqlite
    go:
      package: store
      out: internal/store
      interface:
        enabled: true
        name: Querier
```

The later YAML snippets on this page show only that database's `go` section. Merge them there; `go` is not a top-level configuration key.

`sgl generate` then writes `querier_interface.go`, which lists the method of every query
with the signature the emitter generated, and asserts that `*Queries` implements it. Code
that takes the interface runs against the database, inside a transaction (`WithTx` returns a
`*Queries`) or against a stand-in:

```go
func ArchiveAuthor(ctx context.Context, queries store.Querier, id int64) error {
	author, err := queries.GetAuthor(ctx, id)
	if err != nil {
		return err
	}
	return queries.DeleteAuthor(ctx, author.ID)
}
```

A test's stand-in embeds the interface and implements only the methods the code calls; any
other method panics if called:

```go
type fakeAuthors struct {
	store.Querier
	deleted []int64
}

func (fake *fakeAuthors) GetAuthor(ctx context.Context, id int64) (store.GetAuthorRow, error) {
	return store.GetAuthorRow{ID: id, Name: "Ada"}, nil
}

func (fake *fakeAuthors) DeleteAuthor(ctx context.Context, id int64) error {
	fake.deleted = append(fake.deleted, id)
	return nil
}
```

Mock generators that read an interface, such as `mockgen` in source mode, read
`querier_interface.go` as they would a hand-written one.

## Add struct tags to fields

Give every field that holds a column the same tags with `go.struct_tags`, where `{name}`
stands for the column's name:

```yaml
go:
  struct_tags:
    db: "{name}"
```

Give one column's fields tags of their own with a column override, or with a `sgl.column`
directive in a migration or a query header; a `json` tag among them replaces the generated
one:

```yaml
go:
  types:
    overrides:
      - column: users.email
        struct_tags:
          validate: "required,email"
```

```sql
CREATE TABLE users (id BIGINT PRIMARY KEY, email TEXT NOT NULL, password_hash TEXT NOT NULL);
-- sgl.column(users.password_hash, struct_tags: 'json:"-"')
```

```go
type Users struct {
	ID           int64  `json:"id" db:"id"`
	Email        string `json:"email" db:"email" validate:"required,email"`
	PasswordHash string `json:"-" db:"password_hash"`
}
```

The query header's directive wins over the migration's, the migration's over the override's
and the override's over `go.struct_tags`, key by key. A row field reading the column gets
its tags too, directly, through a view or through a cast. `Params` structs carry no tags.

## Choose the JSON tag style

```yaml
go:
  json_tags:
    case: camel       # snake, camel or pascal
    omitempty: true
```

`author_id` becomes `json:"authorId,omitempty"`. Two columns whose names meet once cased
stay apart (`userName`, `userName2`). `omit: true` leaves the `json` tags out, except where
a column's own struct tags give one.

## Return an empty slice for no rows

```yaml
go:
  empty_slices: true
```

A `many` method, and a runtime builder's `All`, then return `[]Row{}` rather than `nil`
when no row matches, so a handler that encodes the result writes `[]` rather than `null`.

## Return a table's model or a shared row type

A query that selects a table's whole row can return the table's model, the struct
`models.go` declares, instead of a Row struct of its own:

```sql
-- sgl.query(ListAuthors, many, model: authors)
SELECT * FROM authors ORDER BY name;
```

`ListAuthors` then returns `[]Authors`, which a caller can pass wherever it takes an author.
To share a struct between queries that select the same columns, give each the same `row:`
name; the first of them, by file and then by name, declares it:

```sql
-- sgl.query(ListAuthorNames, many, row: AuthorName)
SELECT id, name FROM authors ORDER BY name;

-- sgl.query(SearchAuthorNames, many, row: AuthorName)
SELECT name, id FROM authors WHERE name LIKE $1;
```

The columns must give the struct's fields, by name, of the same Go types and struct tags,
in any order; a query whose columns do not is error [Q065](../reference/diagnostics.md#q065), which
lists what differs. To return the model from every query whose rows are a table's model,
without the keyword, turn on:

```yaml
go:
  reuse_models: true
```

A query reading other columns, such as one joining a second table, keeps its Row struct. See
[rows read into a model or a shared row type](../reference/directives.md#rows-read-into-a-model-or-a-shared-row-type).

## Write doc comments from the SQL

```yaml
go:
  doc_comments: true
```

The prose comment lines of a query's header become its method's doc comment, and the
interface's method's:

```sql
-- GetAuthor reads one author.
--
-- It fails with sql.ErrNoRows when none matches.
-- sgl.query(GetAuthor, one)
SELECT id, name FROM authors WHERE id = $1;
```

A table's or a column's comment becomes the doc comment of its model or field, from
`COMMENT ON TABLE` and `COMMENT ON COLUMN` on PostgreSQL, CockroachDB, TimescaleDB and
DuckDB, and from `COMMENT` clauses on MySQL, MariaDB and ClickHouse. See
[generated Go](../reference/generated-go.md) for the files these settings change and
[configuration](../reference/configuration.md#generated-code-go) for every key.
