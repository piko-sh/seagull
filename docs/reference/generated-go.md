---
title: Generated Go API
description: Generated packages contain ordinary Go source. Keep application logic
  in separate files; generation owns files bearing its generated header.
section: Reference
subsection: Core
order: 210
---

# Generated Go API

Generated packages contain ordinary Go source. Generation owns its output filenames, so
keep application logic in files with different names. Headers identify stale generated
files for pruning; they do not prevent replacement at an output filename.

`sgl generate` writes these files into the output directory configured for the database (`go.out` in `sgl.yaml`):

### `querier.go`

Defines the `DBTX` interface and the `Queries` struct. With database/sql:

```go
type Queries struct { ... }

func New(db DBTX) *Queries
func NewWithReplica(writer DBTX, reader DBTX) *Queries
func (queries *Queries) WithTx(transaction *sql.Tx) *Queries
func (queries *Queries) RunInTx(ctx context.Context, db *sql.DB, fn func(*Queries) error) error
```

What the file provides depends on the emitter:

| | database/sql | pgx | Native ClickHouse |
|---|---|---|---|
| `DBTX` methods | `ExecContext`, `QueryContext`, `QueryRowContext` | `Exec`, `Query`, `QueryRow`, `SendBatch`, `CopyFrom`, `Begin` | `Query`, `QueryRow`, `Exec`, `PrepareBatch` |
| Satisfied by | `*sql.DB`, `*sql.Tx`, `*sql.Conn`, a `PreparedDBTX` | `*pgxpool.Pool`, `*pgx.Conn`, `pgx.Tx` | `driver.Conn` |
| `New(db)` | yes | yes | yes |
| `NewWithReplica(writer, reader)` | yes | no | no |
| `WithTx` | `WithTx(*sql.Tx)` | `WithTx(pgx.Tx)` | no |
| `RunInTx` | `RunInTx(ctx, *sql.DB, fn)` | `RunInTx(ctx, DBTX, fn)` | no |

`NewWithReplica` routes read-only queries to the reader. Writes, writing CTEs,
row-locking SELECTs and calls to functions that may modify data use the writer. The
`readonly:` keyword overrides analysis. See [reader and writer routing](directives.md#reader-and-writer-routing)
for function rules and [replica consistency](../explanation/replicas.md#routing-does-not-provide-consistency)
for the limits of this routing.

With go-sql-driver/mysql (MySQL and MariaDB), the DSN must set `parseTime=true`
so the generated code can read `DATETIME`, `DATE` and `TIMESTAMP` columns into
`time.Time`.

#### Wrapping a transaction: `WrapDBTX`

With database/sql, a `DBTX` passed to `New` or as the writer of `NewWithReplica` may also
implement one more method, which the generated code finds by a type assertion:

```go
WrapDBTX(inner any) any
```

`WithTx`, and so `RunInTx`, calls it with the `*sql.Tx` and runs the transaction's queries
on the `DBTX` it returns, so a wrapper that traces, times or logs queries on the
connection keeps doing so inside a transaction. A wrapper that does not implement the
method, or that returns a value which is not a `DBTX`, leaves the transaction unwrapped.
The `PreparedDBTX` from `prepared.go` implements it to bind its prepared statements to the
transaction. The pgx and native ClickHouse queriers have no such hook: wrap the `pgx.Tx`
or `driver.Conn` yourself before passing it to `WithTx` or `New`.

```go
type tracer struct{ inner store.DBTX }

func (t tracer) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
    log.Printf("exec %s", store.QueryName(query))
    return t.inner.ExecContext(ctx, query, args...)
}

// QueryContext and QueryRowContext follow the same pattern.

func (t tracer) WrapDBTX(inner any) any {
    transaction, ok := inner.(store.DBTX)
    if !ok {
        return inner
    }
    return tracer{inner: transaction}
}
```

The [tracing example](../../examples/sqlite/026_tracing_queries/example.go) runs one end to end.

### `querier_interface.go`

With `go.interface.enabled: true` (`Preset.Interface` when embedding), an interface listing
the method of every query, by name, with each method's doc comment, and an assertion that
`*Queries` implements it:

```go
// Querier lists the method of every query, which Queries implements, so code can depend
// on the methods it calls and a test can stand in for the database.
type Querier interface {
	DeleteAuthor(ctx context.Context, id int64) error
	GetAuthor(ctx context.Context, id int64) (GetAuthorRow, error)
	SearchAuthors() *SearchAuthorsBuilder
}

var _ Querier = (*Queries)(nil)
```

Every emitter writes it, from the methods its query files declare on `*Queries`, so the
signatures are exactly the generated ones: `execresult` returns `sql.Result` with
database/sql and `pgconn.CommandTag` with pgx, and a runtime builder's method returns its
builder. `WithTx` and `RunInTx` are not in it; a transaction's querier, `WithTx(tx)`, is a
`*Queries` and so a `Querier`, which lets a function taking a `Querier` run inside a
transaction or against a test double. `go.interface.name` names it (`Querier` by default);
the name takes precedence over a model, so a table named `querier` gets the model
`Querier2`.

### `models.go`

A Go struct for every table the migrations define, named after the table, with a field per
column named in Go capitalisation. A view has no model, and nor does a partition, whose
rows a query reads into a Row struct, or with `model:` into the model of the table it
partitions. It also declares the named types the queries and tables use: each enum as a string type
with a constant per value and a `Valid` method, and each struct of named fields as a struct
type (see [type mapping](type-mapping.md#defaults)). A nullable column's field is a
`Null[T]`, which `sgl_null.go` declares with `NullOf` and `NullFrom` (see
[NULL values](type-mapping.md#null-values)), unless `go.nullable_pointers` makes it a `*T`.

Every field holding a column carries a `json` tag naming the column as it is written,
made unique in its struct (`id`, `id2`). `go.json_tags` writes the names in `snake`,
`camel` or `pascal` case, adds the `omitempty` option, which a `Null` field takes as
`omitzero`, or leaves the tags out;
`go.struct_tags` adds tags to every field after `json`, such as `db: "{name}"`, and a
column's own tags, from a column override or a
[`sgl.column` directive](directives.md#type-override-sglcolumn), take precedence key by key.
Row structs follow the same rules. With `go.doc_comments: true`, a table's comment documents
its model and a column's comment its field: `COMMENT ON TABLE` and `COMMENT ON COLUMN` on
PostgreSQL, CockroachDB, TimescaleDB and DuckDB (`IS NULL` removes one), a column's
`COMMENT 'text'` and the `COMMENT='text'` table option of `CREATE TABLE` on MySQL and
MariaDB, and a column's or a table's `COMMENT 'text'` on ClickHouse, as the migrations leave
them:

```go
// Posts are the articles authors publish.
type Posts struct {
	ID int64 `json:"id"`
	// The headline, shown in lists.
	Title    string      `json:"title"`
	AuthorID Null[int64] `json:"author_id"`
}
```

### `prepared.go`

A prepared-statement cache for the database/sql emitter. `sgl` writes it only when the
database sets `go.prepared: true`, and nothing uses it until you do:

```go
prepared, err := store.Prepare(ctx, db) // prepares every static query once
if err != nil {
    return err
}
defer prepared.Close()
queries := store.New(prepared)
```

`Prepare` returns a `*PreparedDBTX` that runs each static query through its prepared
statement and sends every other query (one with a slice, optional, sortable or clamped
`LIMIT` parameter, a batch, or a runtime builder) straight to the `*sql.DB`. When a
statement fails to prepare, `Prepare` closes every statement it had prepared and returns
the failure, `preparing statement: ...`, joined with any error closing them, so a failed
start leaves no statement open.

Transactions keep the statements. On a `Queries` built from the `PreparedDBTX`, `WithTx` and
`RunInTx` bind each prepared statement to the transaction with `tx.StmtContext` the first
time the transaction runs it, and reuse that statement for the rest of the transaction, and
send every other query to the transaction directly. `RunInTx` takes the `*sql.DB`, which
`prepared.UnderlyingDB()` returns:

```go
err := queries.RunInTx(ctx, prepared.UnderlyingDB(), func(tx *store.Queries) error {
    _, err := tx.GetOrder(ctx, id) // runs the prepared statement inside the transaction
    return err
})
```

ClickHouse never gets the file: clickhouse-go prepares only `INSERT` statements and rejects
a prepared `SELECT`. `sgl` reports `go.prepared: true` for ClickHouse as a configuration
error, and the library's emitter refuses an explicit
`emit_driver_databasesql.WithPrepared(true)` for it with [Q059](diagnostics.md#q059).
pgx and the native ClickHouse driver take no `go.prepared`; `sgl` reports it as a
configuration error.

The library's emitter has the opposite default: `emit_driver_databasesql.New()` writes
`prepared.go` unless given `emit_driver_databasesql.WithPrepared(false)`, so programs that
called the library before the setting existed keep their output. See
[embedding](generator-api.md#emitters).

### `<name>.sql.go`

One file per query file, named after it. Each contains the typed methods for that file's
queries, and for each query a constant holding its SQL, named after the query in lower
camel case with the preset's suffix (`getUserSQL` for `GetUser`; see
[presets](presets.md)). The SQL is the query as written, without its directive lines and
with every parameter in the dialect's execution form, such as `?1` for SQLite and `$1`
for PostgreSQL, and with each `*` and `table.*` of its select list or `RETURNING` clause
written as the columns it stands for, quoted in the dialect's own way:
`SELECT * FROM users` becomes `SELECT "users"."id", "users"."name" FROM users`.

Explicit column lists preserve the generated projection when a migration adds columns.
The [runtime explanation](../explanation/generated-runtime.md#schema-changes-meet-deployed-code)
describes their role in rolling deployments. A star stays as written where its columns cannot be named as
the engine names them: over a common table expression, a derived table, a table-valued
function or `VALUES`, where the engine may name a computed column otherwise, and in the
branches of a `UNION`, `INTERSECT` or `EXCEPT`. In a `RETURNING` clause an unqualified
star names a column alone in place of the alias of the table the statement changes,
since PostgreSQL takes only the alias there and SQLite only the table's name. Set
`go.keep_stars: true` (`Preset.KeepStars` when embedding) to write every star as the
query writes it; a star the driver needs a column of read through a wrap, such as an
array read as JSON behind `database/sql`, is still expanded.

Each query that returns rows declares a Row struct, `<Name>Row`, with a field per column,
unless its header names the struct its rows are read into: `model: users` reads them into
the `Users` model `models.go` declares, and `row: UserSummary` into a struct of that name,
which the file of the first query naming it declares and every query naming it shares. With
`go.reuse_models: true` (`Preset.ReuseModels`) a query whose rows are a table's model reads
them into the model without the keyword. See
[rows read into a model or a shared row type](directives.md#rows-read-into-a-model-or-a-shared-row-type).

A method takes its parameters as arguments, or as one `<Name>Params` struct when there are
several. A nil slice passed for an array or list parameter binds `NULL` and an empty one an
empty array, except on ClickHouse, whose arrays cannot be `NULL`, and except where the value
is written into a column that refuses `NULL`, where a nil slice or `[]byte` binds the empty
value; see
[nil and empty slices](type-mapping.md#nil-and-empty-slices).

A `many` method returns `nil` when no row matches, which `encoding/json` writes as `null`;
with `go.empty_slices: true` (`Preset.EmptySlices`) it returns an empty slice, written as
`[]`, and so does a runtime builder's `All`. A grouped `many` always returns an empty slice.
With `go.doc_comments: true`, each method carries its query's description, the prose
comment lines of its header (see [where directives go](directives.md#where-directives-go)),
as its doc comment.

A `stream` method returns `iter.Seq2[<Name>Row, error]` under the default preset
(`Preset.ModernIdioms`), and `func(func(<Name>Row, error) bool)`, the same underlying
type, with that setting off; `for row, err := range` reads either. The default preset
also writes a blank line between each two top-level declarations of every generated file.

With pgx, a `batch` method queues its items in one `pgx.Batch`. It returns the error of
the first statement that fails, joined with any other error closing the batch reports, and
otherwise the error pgx reports when it closes the batch, such as a deferred constraint
that fails as the batch commits; see [batch execution](query-commands.md#batch-execution).

#### Labelled errors: `WrapErrors`

With `Preset.WrapErrors` (`emit.Style.WrapErrors` to an emitter), every error a generated
method returns is labelled with the error prefix and the method's name, and the package
gains `error_wrapping.go`:

```go
func (queries *Queries) GetUser(ctx context.Context, id int64) (GetUserRow, error) {
	var row GetUserRow
	err := queries.reader.QueryRowContext(ctx, getUserSQL, id).Scan(&row.ID, &row.Name)
	if err != nil {
		return GetUserRow{}, sglWrapError("sgl: GetUser", err)
	}
	return row, nil
}
```

`sglWrapError` returns nil for a nil error and `fmt.Errorf("%s: %w", label, err)`
otherwise, so a missing row reads `sgl: GetUser: sql: no rows in result set` and
`errors.Is(err, sql.ErrNoRows)` still matches. A method returning a call's two results
passes them through `sglWrapResult`, a `stream` method labels the error it yields, and a
runtime builder labels its terminals `SearchUsers.All`, `SearchUsers.One` and
`SearchUsers.Count`, its allow-list errors included. A `batch` or `copyfrom` method sent
as chunked multi-row statements names the row its failing statement starts at, counted
from zero, so the rows before it are the ones already written:
`sgl: AddUsers: the statement starting at row 499: ...`. The setting is off by default;
turn it on with `go.wrap_errors: true` in `sgl.yaml` or `Preset.WrapErrors` in Go.

A query file whose name Go would ignore or read as a build constraint
gets a safe name: `_private.sql` gives `q_private.sql.go`, and `devices_ios.sql` gives
`devices_ios_sql.sql.go`.

Every name the files declare comes from one naming plan for the package, so a query, a
parameter or a table never takes a name the generated code already uses: a parameter
called `ctx` is passed as `ctx2`, and a table called `queries` gets the model `Queries2`.
A doc comment taken from a description or a schema comment writes a NUL, a byte order
mark or a byte that is not valid UTF-8 as U+FFFD, which Go source can hold.

Generated queries include the helpers they need, so they do not import Seagull.
Helper names use the preset's prefix (`sgl` by default).

### Choosing the JSON codec: `SetJSONCodec`

A generated package that uses JSON declares `JSONCodec` and `SetJSONCodec` in
`sgl_json_codec.go`. It uses `encoding/json` until you install another codec:

```go
type JSONCodec interface {
	Marshal(value any) ([]byte, error)
	Unmarshal(data []byte, value any) error
}

func SetJSONCodec(codec JSONCodec)
```

`SetJSONCodec(nil)` restores `encoding/json`. The codec is held atomically, so it is safe
to set while queries run. Each value uses the codec installed when it is encoded or
decoded. A codec with a `Pretouch(reflect.Type) error` method receives the JSON column types
listed in `sgl_json_codec.go` before installation. A `Pretouch` error does not stop
installation.

The codec must reject input that is not exactly one JSON document. The generated code
still uses `encoding/json` to check nullable array and object values against their Go
types, and to read array elements written in an engine's own form.
