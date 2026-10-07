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
type (see [type mapping](type-mapping.md#defaults)).

Every field holding a column carries a `json` tag naming the column as it is written,
made unique in its struct (`id`, `id2`). `go.json_tags` writes the names in `snake`,
`camel` or `pascal` case, adds the `omitempty` option, or leaves the tags out;
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
	Title    string `json:"title"`
	AuthorID *int64 `json:"author_id"`
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
`RunInTx` bind each prepared statement to the transaction with `tx.StmtContext`, and send
every other query to the transaction directly. `RunInTx` takes the `*sql.DB`, which
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
empty array, except on ClickHouse, whose arrays cannot be `NULL`; see
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

### Helper files

Query helpers are written into the package, so generated queries do not import the
Seagull generator. Complex-value adapters and runtime-filter conversion can use
reflection; ordinary row methods scan into known fields. Each file appears only when a query needs it, and its
identifiers start with the preset's helper prefix (`sgl` by default):

<!-- generated-file-table: helpers (make generate-all) -->
| File | When | Contents |
|---|---|---|
| `sortable.go` | A query is sortable | `OrderDirection` with `OrderAsc` and `OrderDesc`, declared once for the package. |
| `bind_limits.go` | A query expands a slice or is built at run time | The engine's bind-variable limit, `ErrSglTooManyBindVariables`, and the splicer that edits a query's SQL at offsets fixed when the code was generated. |
| `slice_helpers.go` | A query expands a slice | The list writer: a placeholder per element, or the dialect's test for an empty list. |
| `runtime_helpers.go` | A query is built at run time | The builder's allowed operators, conversions, filters, ordering and pagination, in the dialect's syntax, and `ErrSglUnknownColumn`, `ErrSglUnknownOperator` and `ErrSglUnknownDirection`. |
| `sgl_json.go` | A JSON value keeps the default JSON slot | The `JSON` type and its scan adapter. The file is named after the helper prefix. |
| `driver_values.go` | database/sql, where the driver needs a parameter converted; pgx, where a binary column's override type binds its own bytes | Conversions of the parameters a driver does not bind as they are, such as nullable decimals and the bytes a binary override marshals itself to. |
| `driver_composites.go` | database/sql, where a driver binds or returns lists, structs or maps in a form of its own, or a column whose Go type an override chose needs reading through the adapter | The scan adapter reading those forms and the one reading an overridden column (see [what an override type must support](type-mapping.md#what-an-override-type-must-support)), and the conversion of a list, struct or map parameter into the driver's form, which refuses a value nesting more than 32 levels deep or referring to itself. |
| `clickhouse_values.go` | A ClickHouse query binds a parameter by name, or a native batch writes `clickhouse_appending.go` | The formatter writing each value in the shape its ClickHouse type takes, and the parser of those shapes. |
| `clickhouse_appending.go` | A native ClickHouse batch or copyfrom appends a value holding an IP address, a map of tuples or an overridden Go type | The conversion of each row into the forms clickhouse-go appends, refusing a bad address before the batch sends anything. It reads shapes with the parser in `clickhouse_values.go`, so that file always comes with it. |
| `clickhouse_composites.go` | A native ClickHouse read needs a column converted: an array, map or tuple holding IP addresses, wide integers or JSON, a JSON column, or an overridden Go type | The converting scan adapter: the driver fills the form it reads, and the adapter converts it into the generated field after the scan. |
| `clickhouse_documents.go` | A ClickHouse query reads a JSON value, on either emitter | The conversion of the documents clickhouse-go decodes into the JSON slot's type or an override's type, giving the same bytes on both emitters. |
| `value_reading.go` | A query reads a column through the scan adapter in `driver_composites.go` or `clickhouse_composites.go` | The reflection readers both emitters share: a list into a slice, a map, a Go map or the driver's ordered one, into a map, and a scalar into a scalar of its kind, each element through the driver's own reader, refusing an integer its target cannot hold. A nil list or map reads as nil. |
| `error_wrapping.go` | The preset sets `WrapErrors` | `sglWrapError` and `sglWrapResult`, which label each error a generated method returns with the error prefix and the method's name, keeping the error beneath for `errors.Is` and `errors.As` (see [labelled errors](#labelled-errors-wraperrors)). |
| `convert_strings.go` | A driver binds or reads an enum only as plain strings: pgx, pgx's stdlib behind database/sql, the native ClickHouse driver, or ClickHouse behind database/sql | Conversions between an enum's named type and plain strings, in slices, slices of pointers and maps. |
| `query_names.go` | The preset enables the query-names file, under the name it gives | The function mapping a statement's SQL to its query's name, for labelling telemetry, including SQL a method edits at run time. |
<!-- end generated-file-table -->
