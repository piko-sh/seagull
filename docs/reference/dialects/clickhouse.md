---
title: ClickHouse
description: Seagull supports ClickHouse as a first-class engine.
section: Reference
subsection: Dialects
order: 180
---

# ClickHouse

The ClickHouse dialect understands typed named parameters, type wrappers, MergeTree
metadata and query extensions. Driver representation and asynchronous mutation behaviour
differ from the PostgreSQL and MySQL families; the contracts below describe those
choices.

## Capability summary

| Concern | Contract |
|---|---|
| Parser family | ClickHouse |
| Generated API | database/sql or native clickhouse |
| Live introspection | No built-in source |
| Runtime migrations | Migrations do not use transactions. Asynchronous mutation acceptance is separate from completion. |

[Known limitations](../limitations.md#clickhouse) records inference gaps and workarounds; the
[driver matrix](../driver-matrix.md) compares generated APIs.

The [engine guide](../../how-to/clickhouse.md) and [runnable examples](../../../examples/clickhouse) cover execution. The declarations below link to constructor contracts. Parser limits affect analysis, not server execution.

## Quick start

Wire ClickHouse into your application config:

```yaml
databases:
  - name: analytics
    dialect: clickhouse
    migrations: db/migrations
    queries: db/queries
    go:
      package: analytics
      out: internal/analytics
      driver: clickhouse        # or database/sql
```

The `clickhouse` dialect brings:

- The driver name `clickhouse` (registered by the
  `github.com/ClickHouse/clickhouse-go/v2` package).
- The ClickHouse dialect (parser, type system, function
  catalogue).
- `migrate_backend_clickhouse.New()` for migration execution, which
  runs each statement separately (ClickHouse has no DDL transactions)
  and uses no-op locking (ClickHouse has no advisory locks).

Neither driver gets a `prepared.go`. clickhouse-go prepares only
`INSERT` statements, so it cannot prepare the package's queries: `sgl`
reports `go.prepared: true` as a configuration error with either driver,
and the library's database/sql emitter refuses an explicit
`emit_driver_databasesql.WithPrepared(true)` with [Q059](../diagnostics.md#q059)
(its default leaves the cache out without a word).

`batch` and `copyfrom` both return `error`. database/sql sends `batch`
and `copyfrom` as multi-row `INSERT`s with typed per-row placeholders,
at most 1024 values a statement. The native driver appends a `batch`
to a `PrepareBatch` when every item of its `VALUES` row is a placeholder
alone, and otherwise sends multi-row `INSERT`s as database/sql does, so
expressions in the row are kept. A native `copyfrom` always appends to a
`PrepareBatch`, so every item of its `VALUES` row must be a placeholder
alone ([Q060](../diagnostics.md#q060) otherwise). The native
`Queries` has only `New`, with no `WithTx`, `RunInTx` or
`NewWithReplica`.

## Named parameters: `{name:Type}`

ClickHouse's native protocol uses brace-delimited parameters with an
embedded type tag. Seagull keeps the `{name:Type}` brace form (it does
not rewrite to positional placeholders). Seagull does rewrite each
parameter name to a `p_`-prefixed wire name, so reserved keywords
like `limit` and `offset` cannot reach the server as parameter names.

```sql
-- sgl.query(GetUser, one)
SELECT id, email FROM users WHERE id = {user_id:UInt64};
```

Seagull sends `{user_id:UInt64}` to the server as `{p_user_id:UInt64}`
and the generated Go method binds the value as
`clickhouse.Named("p_user_id", sglClickHouseValue(userID, "n"))`, the text the generated
formatter in `clickhouse_values.go` writes for a `UInt64`, with either driver: the native emitter passes it to the connection's `Query` or
`Exec`, and the database/sql emitter passes it as a named argument
to `QueryContext` or `ExecContext`:

```go
user, err := queries.GetUser(ctx, 42)
```

A list filter needs no `kind: slice`. Compare with an `Array`
parameter and the method takes a Go slice:

```sql
-- sgl.query(EventsByIDs, many)
SELECT id, tag FROM events WHERE id IN {ids:Array(UInt64)};
```

```go
func (queries *Queries) EventsByIDs(ctx context.Context, ids []uint64) ([]EventsByIDsRow, error)
```

`Map` and named `Tuple` parameters bind the same way. A named `Tuple` parameter takes the
type of the Struct slot: an anonymous struct with the native driver and `map[string]any`
behind database/sql, keyed by field name. The generated code sends its fields in the order
the parameter's type declares them, so the order of a map's keys does not matter:

```sql
-- sgl.query(KioskAt, many)
SELECT id FROM kiosks WHERE location = {location:Tuple(lat Float64, lon Float64)};
```

With `driver: database/sql`, pass a map:

```go
kiosks, err := queries.KioskAt(ctx, map[string]any{"lon": -4.9389, "lat": 50.5413})
```

With the native driver, pass the generated anonymous struct instead; the
[composite-parameter integration caller](../../../tests/integration/clickhouse/testdata/079_composite_parameters_native/runner.go)
constructs it through the `Location` field of `OpenKioskParams`.

The same `{name:Type}` token can appear multiple times in one query.
All occurrences bind to the single Go parameter:

```sql
-- sgl.query(FindLinked, many)
SELECT id FROM nodes WHERE id = {nid:UInt64} OR parent_id = {nid:UInt64};
```

A placeholder may also be an argument of a table function, as in
`SELECT number FROM numbers({n:UInt64})`, where it is bound like any other and typed by its
own tag.

A directive binds to the whole token. Optional and sortable queries bind by name like any
other, with both drivers:

```sql
-- sgl.query(SearchTrips, many)
-- {destination:Nullable(String)} as sgl.param(destination, optional: true)
-- sgl.sortable(order_by, [name, price_pence])
SELECT id, name, price_pence FROM trips
WHERE ({destination:Nullable(String)} IS NULL OR destination = {destination:Nullable(String)})
LIMIT {page_size:UInt32};
```

A `dynamic: runtime` builder works with both drivers too, including `Count()`.

A placeholder that is the whole row count of `LIMIT`, `FETCH` or `LIMIT m, n`, or the whole
offset of `OFFSET`, is a pagination parameter, as on every other dialect: its field is an
`int`, and a `default:` or `max:` on its binding line is applied before the query runs.

```sql
-- sgl.query(ListFinds, many)
-- {page_size:UInt32} as sgl.param(page_size, default: 3, max: 5)
SELECT id, creature FROM finds ORDER BY id LIMIT {page_size:UInt32} OFFSET {page_offset:UInt32};
```

`ListFinds` with a page size of 0 reads three rows, and with 50 reads five.

### IP address parameters

Invalid `IPv4` and `IPv6` values are refused, including values inside an `Array`, a `Map`
or a `Tuple`. Where the error comes from depends on how the method binds the value:

- A native `copyfrom` or `batch` of lone placeholders checks the row locally and returns
  an error before sending the batch.
- A named parameter on either driver sends text carrying the refusal reason; the server
  rejects the statement and reports that reason.

Both paths name the parameter and refuse:

- the zero `netip.Addr`, and a nil or empty `net.IP`, which hold no address;
- a `net.IP` that is neither four nor sixteen bytes long;
- an IPv6 address for an `IPv4` value, which cannot hold one;
- with `driver: clickhouse`, text that is not an address, where a type override makes the
  address a string type, such as `json.Number`, which is appended as its text.

An IPv4-mapped IPv6 address such as `::ffff:10.0.0.1` is stored as IPv4 in an IPv4
value; an IPv4 address is stored IPv4-mapped in an IPv6 value. Zones such as `%eth0` are
dropped. A locally refused native batch writes no rows. A multi-statement batch can
retain earlier successful statements when a later statement is refused.

`NULL` in a `Nullable(IPv4)` or `Nullable(IPv6)` value is a nil pointer, a `*netip.Addr`
(a `*net.IP` behind `database/sql`), which is how the generated code reads it. A pointer to
the zero address is refused rather than written as `NULL`: the read side never returns one,
so every value the generated code reads is written back as it was read, and an address left
unset by mistake fails instead of silently becoming `NULL`.

A column override may read an address as a `netip.Addr` or as its text in a string type, such
as `encoding/json.Number`, with either driver. clickhouse-go hands `database/sql` an address
as a `net.IP`, which `database/sql` stores in neither, and the native driver scans an address
only into its own types, so the generated code reads such a column through its override scan
adapter, which converts the address (see [what an override type must
support](../type-mapping.md#what-an-override-type-must-support)).

### Map literals in `INSERT ... VALUES`

ClickHouse reads the rows of `INSERT ... VALUES` as data, where `{key: value, ...}` is a
`Map`: `{'apples': 5}`, `{1: 'x'}`, the empty `{}`, and maps inside arrays, tuples and
other maps. A `{` starts a placeholder only when a name follows it, so a map literal and a
placeholder can share a row, and a `batch` repeats the literal in every row it writes:

```sql
-- sgl.query(RecordStock, exec)
INSERT INTO stock (id, counts, shelves) VALUES ({id:UInt64}, {'apples': 5, 'pears': 2}, [{'top': 1}, {}]);
```

Elsewhere ClickHouse reads `{` only as a placeholder, so a map literal in a select list, a
condition, an `INSERT ... SELECT` or a mutation is a syntax error on the server, and
Seagull refuses it with [Q010](../diagnostics.md#q010) at its brace, as it refuses a
stray `}`; write `map('apples', 5)` there.
A map literal holds data rather than expressions, and the server substitutes no
placeholder inside one, so `{'kiwis': {kiwis:UInt32}}` is refused too: write
`map('kiwis', {kiwis:UInt32})`, or bind the whole map as `{counts:Map(String, UInt32)}`.
Seagull does not check a literal's keys and values otherwise; the server rejects an
expression such as `1 + 1` in one. A `{` followed by a name and a colon is a placeholder
only when a type name follows the colon, as ClickHouse reads it, so `{true: 1}` and
`{false: 0}` in a `Map(Bool, UInt8)` row are map literals. So is `{true: false}` in a
`Map(Bool, Bool)` row: the words `true`, `false`, `null`, `nan`, `inf` and `infinity`, in
any case, are values and name no type, so a brace whose key and value are both such words
holds data. `{flag:Bool}` and `{true:UInt8}` stay placeholders.

The rows after `INSERT ... FORMAT` are the format's own data, so inline `JSONEachRow`
rows such as `{"id": 9}` keep their braces too.

ClickHouse itself (verified on 25.4) evaluates a tuple in a `VALUES` row that holds a
function call, such as `(170141183460469231731687303715884105727, map('k', 1))`, as an
expression, reading a bare integer literal beyond the `UInt64` range there as a `Float64`:
the row goes in without an error, holding another number or the whole tuple's default,
`(0, {})`. Write such a literal through a conversion, as
`toInt128('170141183460469231731687303715884105727')` or `toUInt256('...')` does.

## Type wrappers

ClickHouse encodes wrappers inside the type name. Seagull strips them
during normalisation and surfaces the resulting Go type:

| Type declaration | Generated Go field |
|---|---|
| `UInt64` | `uint64` |
| `Int32` | `int32` |
| `Nullable(String)` | `*string` |
| `Array(String)` | `[]string` |
| `Array(Nullable(UInt32))` | `[]*uint32` |
| `LowCardinality(String)` | `string` (storage hint only) |
| `Float32` | `float32` |
| `Bool` | `bool`; a `UInt8` flag, as older schemas declare one, stays a `uint8` |
| `Tuple(String, UInt64)` | `[]any`, fields in order; [nested tuples](#nested-tuples) have restrictions. |
| `Tuple(name String, age UInt64)` | Named fields in a struct natively; `map[string]any` behind database/sql. |
| `Map(String, UInt32)` | `map[string]uint32` |
| `Map(String, Tuple(a UInt8, b String))` | Map of structs natively; `map[string]map[string]any` behind database/sql. See [map results](#maps-of-named-tuples). |
| `IPv4`, `IPv6` | `netip.Addr` natively; `net.IP` behind database/sql; pointers when Nullable. See [address binding](#ip-address-parameters). |
| `Nested(name String, age UInt64)` | one field per stored column, `[]string` and `[]uint64`; see [Nested columns](#nested-columns) |
| `DateTime64(6, 'UTC')` | `time.Time` |
| `Time`, `Time64(6)` (ClickHouse 25.6 and later) | `time.Duration`, `*time.Duration` when `Nullable`: both drivers read the span, which may be negative and pass 24 hours, and a parameter binds one as `HHH:MM:SS.ffffff`; creating such a column needs `SET enable_time_time64_type = 1` (25.8), which a migration may hold |
| `Decimal(18, 4)` | high-precision decimal type |
| `Enum8('a' = 1, 'b' = 2)` | a named string type in `models.go`, such as `EventsKind` for `events.kind`, with a constant per value; `string` when the Enum slot is `string` (see [enums](../type-mapping.md)) |
| `UUID` | `github.com/google/uuid.UUID` |
| `JSON`, `Nullable(JSON)` | the generated `JSON` type, nil for `NULL`; see [JSON columns](#json-columns) |

An `Enum8` or `Enum16` label may hold a doubled quote or a backslash escape, as in
`'it''s'` or `'x\'y'`, in a column's type and in a placeholder's tag; the label is the
text the server returns.

Nullable scalar values generally become pointers, such as `*string`. Nil-capable types
can represent NULL directly: `Nullable(JSON)` remains `JSON`, with nil for NULL.
`LowCardinality(T)` keeps the Go representation of `T`.

Generated enum types bind and scan through strings where the driver requires them.
Deeper enum combinations can be refused with [Q059](../diagnostics.md#q059), including
maps keyed by enums and some nested batch values. Use `types.enum: string` or convert the
SQL value; [enum mapping](../type-mapping.md#defaults) describes each emitter's supported
shapes. Named parameters are formatted as text regardless of their shape.

## CREATE TABLE engine clauses

ClickHouse's `CREATE TABLE` carries query-planning metadata after the
column list. Seagull parses the whole declaration:

```sql
CREATE TABLE events (
    id UInt64,
    ts DateTime,
    payload String
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, id)
SETTINGS index_granularity = 8192
TTL ts + INTERVAL 90 DAY;
```

Seagull captures the `ENGINE`, `PARTITION BY`, `ORDER BY`, `PRIMARY KEY`, `SAMPLE BY`,
`SETTINGS`, `TTL` and `COMMENT` clauses verbatim into the catalogue mutation's
`EngineSpecific` map, where a tool reading the catalogue can inspect them without
re-parsing the SQL. When your migration declares no explicit `PRIMARY KEY`, Seagull
infers it from the `ORDER BY` column list to match ClickHouse's own behaviour.

Seagull accepts any engine declaration and records its text. The parameters of
`ReplacingMergeTree`, `CollapsingMergeTree`, `VersionedCollapsingMergeTree`,
`SummingMergeTree`, `ReplicatedMergeTree` and `ReplicatedReplacingMergeTree`, such as a
version or sign column, are recorded under keys of their own. The engine does not change
how a query is typed.

### Hidden columns

A `MATERIALIZED`, `ALIAS` or `EPHEMERAL` column is hidden, as ClickHouse hides it: the
table's model, `SELECT *` and an `INSERT` without a column list leave it out, while a query
may name a `MATERIALIZED` or `ALIAS` column and an `INSERT` may write an `EPHEMERAL` one.
Writing a `MATERIALIZED` or `ALIAS` column is [Q013](../diagnostics.md#q013).
ClickHouse cannot read an `EPHEMERAL` column, which Seagull does not report; see
[known limitations](../limitations.md#clickhouse).

## SELECT extensions

A column read outside every aggregate of a query that aggregates without `GROUP BY`, as
in `SELECT title, count() FROM books`, is [Q062](../diagnostics.md#q062), an error,
since ClickHouse rejects the query.

ClickHouse-specific SELECT clauses parse cleanly. Code generation
emits them in the SQL passed to the driver:

```sql
-- sgl.query(LatestVersion, one)
SELECT id, val FROM versioned FINAL WHERE id = {id:UInt64};

-- sgl.query(EventsByTag, many)
SELECT id, tag FROM events ARRAY JOIN tags AS tag WHERE tag = {filter_tag:String};

-- sgl.query(SampledStats, many)
SELECT category, count(*) AS c FROM events SAMPLE 0.1 GROUP BY category;

-- sgl.query(RegionalFiltered, many)
SELECT id FROM events PREWHERE region = {region:String} WHERE ts > {start:DateTime};

-- sgl.query(LatestPerCategory, many)
SELECT id, cat, ts FROM events ORDER BY cat, ts DESC LIMIT 3 BY cat;
```

A heredoc, `$$...$$` or `$tag$...$tag$`, is a `String` read verbatim up to its closing
tag, so a quote, a `;`, a backslash or a placeholder inside it is text: neither the
statement splitter nor the parameter scan reads into it. A tag starts with a letter or an
underscore.

clickhouse-go does not know heredocs. A query whose values it binds by position is bound
on the client by clickhouse-go's own binder (`bind.go`, v2.48), which leaves quoted text,
quoted names and comments alone but reads into a heredoc. The generated code binds by
position only for a query with `dynamic: runtime`, whose runtime builder writes `$1`, `$2` and
so on and passes the values in order, with either driver: there a `$1` written inside a
heredoc is replaced by the first value, and a `?` inside one fails the call with
clickhouse-go's error for mixed placeholder forms. Every other query sends its
`{name:Type}` parameters for the server to bind and is not affected. Keep a `$` followed
by a digit, and `?`, out of the heredocs of a query with `dynamic: runtime`.

JOIN variants: `INNER`, `LEFT`, `RIGHT`, `FULL`, `CROSS`, plus
ClickHouse's `ASOF`, `SEMI`, and `ANTI` variants and the `ANY` and `ALL` strictness
words. `ARRAY JOIN` and `LEFT ARRAY JOIN` both parse; see [ARRAY JOIN](#array-join).

A select item may name an earlier item of the same select list by its alias, as in
`SELECT id + 1 AS next_id, next_id * 2 AS doubled`: the later item is typed from the
earlier one, here `UInt64`, and its nullability follows. An alias is visible in `WHERE`,
`GROUP BY`, `HAVING`, `ORDER BY` and the other clauses too. As ClickHouse does under its
default `prefer_column_name_to_alias = 0`, an alias wins over a column of the same name:
in `SELECT id + 1 AS name, name FROM t WHERE name > 1 ORDER BY name` every `name` after
the first item reads the alias and is a `UInt64`, while `t.name` reads the column and the
item giving the alias reads the column inside its own expression. ClickHouse makes an
alias visible to the items before it as well; Seagull does not, so an item naming the
alias of a later item reads a column of that name, or is reported as an unknown column
when there is none: give aliases names no column has, or order the items so an alias comes
first.

### ARRAY JOIN

`ARRAY JOIN` unfolds an array into one row per element, and Seagull types each name as the
server does:

- Without an alias, as in `ARRAY JOIN tags`, the column itself reads the element:
  `tags` is a `String` for an `Array(String)` column, and `SELECT *` lists the element
  where it lists the column. A `Nested` column unfolds with its fields, so after
  `ARRAY JOIN legs` the field `legs.colony` is a `String` and `legs` a
  `Tuple(colony String, minutes UInt16)`; `ARRAY JOIN legs.colony` unfolds that field
  alone. The column may be qualified, as in `ARRAY JOIN f.legs`.
- With an alias, as in `ARRAY JOIN tags AS tag`, the alias reads the element and the
  column stays an array. `SELECT *` leaves the alias out. A field of a tuple element is
  read through the alias by name or by position: after `ARRAY JOIN legs AS leg`,
  `leg.colony` is a `String`, and so is `leg.1`.
- An expression, as in `ARRAY JOIN [1, 2, 3] AS level` or
  `ARRAY JOIN arrayMap(m -> m * 2, legs.minutes) AS doubled`, is typed by its
  expression.
- `LEFT ARRAY JOIN` keeps a row whose array is empty and gives its element the default
  value of the element's type, an empty string or `0`, not `NULL`, so the element is a
  pointer only when its type is `Nullable(T)`.
- A `Map` unfolds into one `Tuple(keys K, values V)` per entry, as ClickHouse gives it: a
  struct with fields `Keys` and `Values` (`driver: clickhouse`), a `map[string]any` behind
  `database/sql`. After `ARRAY JOIN attributes AS entry`, `entry.keys` and `entry.1` are the
  key; after `ARRAY JOIN attributes` in place, `attributes.keys` and `attributes.values`
  are the key and the value. A `LEFT ARRAY JOIN` gives an empty map's row the tuple's
  default, an empty key and a zero value.

### Outer joins and `join_use_nulls`

ClickHouse fills the columns of an unmatched outer-join row according to the
`join_use_nulls` setting. With the server's default, `0`, an unmatched row gets each
column's default value, such as `0` or an empty string, rather than `NULL`. Seagull
assumes that default: a column on the side a `LEFT`, `RIGHT` or `FULL` join can leave
unmatched keeps its own nullability, so it is a pointer only when its type is
`Nullable(T)`.

A query that sets the option itself is analysed with its own setting:

```sql
-- sgl.query(SightingsWithNulls, many)
SELECT g.id, s.beach FROM gulls AS g
LEFT JOIN sightings AS s ON s.gull_id = g.id
SETTINGS join_use_nulls = 1;
```

Here `Beach` is a `*string`, `nil` for a gull with no sighting. When the server, the
user's profile or the connection sets `join_use_nulls = 1` for every query, say so in the
database's dialect options, and every outer join is analysed that way:

```yaml
databases:
  - name: analytics
    dialect: clickhouse
    dialect_options:
      join_use_nulls: true
```

In Go, pass `dialect_engine_clickhouse.WithJoinUseNulls(true)` to `dialect_engine_clickhouse.New`.

### Dotted names

`a.b` names column `b` of table or alias `a` when one is in scope. Otherwise, as in
ClickHouse, it reads element `b` of column `a`: a field of a named `Tuple`, or a path in a
`JSON` column, so `SELECT basket.fish FROM orders` keeps the column and types it from the
tuple's field, or as `any` for a `JSON` path.

Subcolumns are typed as the server types them: `size0` (and `size1`, ... for nested
arrays) of an `Array` and of a `Map` is a `UInt64`; `keys` and `values` of a `Map` are
arrays of its key and value types; `null` of a `Nullable` column is a `UInt8`, and of an
`Array(Nullable(T))` an `Array(UInt8)`; and a field of a `Nested` column is an array of
the field's type. An unaliased element is named by its path, as ClickHouse names it:
`SELECT legs.colony FROM flights` returns a column named `legs.colony`, and
`SELECT f.id FROM flights AS f` one named `id`. A field of a tuple may also be read by its
position, so `location.1` reads the first field of a named tuple as well as of an
unnamed one, and `pair.2.1` the first field of the tuple in `pair`'s second. Such a read
is resolved wherever it is written, inside `IS NULL`, `IN`, `BETWEEN`, a subscript or a
window function included, and a column `ARRAY JOIN` unfolds in place reads as its element
inside a lambda's body too.

### Nested tuples

clickhouse-go reads an unnamed `Tuple` into a `[]any` only where it reads into the Go type
the generated code gives: the column itself, an element of an `Array`, and a value of a
`Map`. Inside another tuple it reads the elements into interface values, where the native
driver cannot read an unnamed tuple and `database/sql` returns `NULL` or keeps only the
last element, under the empty name. A query that returns an unnamed tuple there, directly
or through an `Array`, as in `Tuple(UInt8, Tuple(String, UInt16))`,
`Tuple(a UInt8, b Tuple(String, UInt16))`, `Array(Tuple(UInt8, Tuple(String, UInt16)))`
or `(id, (id, 'gull'))`, is refused with `Q059` at its select-list item. Name the inner
tuple's elements, as `CAST(pair AS Tuple(UInt8, Tuple(s String, n UInt16)))` does, or
select its elements as columns of their own, as `pair.2.1` does. A named tuple inside an
unnamed one, and a `Map` of unnamed tuples anywhere, read as they are.

Behind `database/sql`, clickhouse-go reads every field of a named `Tuple` into interface
values too, so the `database/sql` emitter also refuses, with `Q059`, an `Array` of unnamed
tuples inside a named tuple, as in `Tuple(a UInt8, b Array(Tuple(String, UInt16)))`,
which it would read with the inner tuples' elements lost; the native driver reads it.
Name the inner tuple's elements there as well.

### `untuple`

`untuple(t)` spreads a tuple into its elements. In a function's arguments, as in
`concat(untuple(pair))`, each element is an argument, which Seagull reads; as an item of a
select list it gives a column for each element, named after the tuple and the element, which
Seagull does not type, so such an item is [Q010](../diagnostics.md#q010). Select the
elements as columns of their own instead, as `pair.1, pair.2` or
`tupleElement(pair, 'name')` do.

### Tuples built with `AS`

An expression in a list may carry an alias, as in `tuple(1 AS x, 'a' AS y)`,
`(id AS key, name AS label)`, `plus(id AS key, 1)` or `[1 AS first, 2]`. The alias leaves
the expression's type as it is, and ClickHouse 25.4, with
`enable_named_columns_in_function_tuple` off as it is by default, names no element of a
tuple after one: `toTypeName(tuple(1 AS x, 'a' AS y))` is `Tuple(UInt8, String)`. Such a
tuple is unnamed, read by position into a `[]any` with both drivers, inside an `Array` or
a `Map` too. Name the elements with a cast, as `CAST(tuple(id, name) AS Tuple(key UInt64,
label String))` does. Seagull assumes the setting is off: a query that turns it on gets
named tuples from the server that the generated `[]any` cannot read behind `database/sql`.

### Maps of named tuples

clickhouse-go's native driver reads a `Map` whose values hold a named tuple, as in
`Map(String, Tuple(a UInt8, b String))`, only as a map of its own types,
`map[string]map[string]any`, never into the generated struct, though it hands that value to
a `sql.Scanner`. With `driver: clickhouse` the generated code therefore reads such a column
through `sglScanNativeComposite`, a converting scan adapter in `clickhouse_composites.go`
that fills the generated maps, slices and structs from it: a `Map` of named tuples, of
arrays or maps of them, an `Array` of such maps, and an `Array` or `Map` of named tuples
that hold one. A struct field is matched as the driver matches one, by its `json` name.
The file is generated only when a query reads such a column.

The driver also builds some values inside a composite in Go types of its own, which it
cannot store into the generated ones: an IP address inside an `Array`, a `Map` or a named
`Tuple` is a `net.IP` (a `*net.IP` when `Nullable`) where the generated code holds a
`netip.Addr`, and a 128- or 256-bit integer that cannot be `NULL`, as an element of a
named `Tuple`, is a `big.Int` value where the generated code holds a `*big.Int`. An
`Array` or `Map` column holding one, such as `Array(IPv4)`, `Map(String, IPv6)` or
`Array(Tuple(a UInt8, big Int128))`, is read through the same converting scan adapter,
which reads a four-byte address as IPv4 and a sixteen-byte one as IPv6, as the driver reads
an address column itself. An address or wide integer column of its own is read as it is.

The driver does not consult a `sql.Scanner` for a tuple column, but it fills a
`map[string]any` keyed by element name with its own values. A named `Tuple` column holding
any of these values, as `holder Tuple(a UInt8, m Map(String, Tuple(x UInt8)))` and
`Tuple(ip IPv4, big Int128)` do, is therefore scanned into such a map and converted into the
generated struct after the scan by `sglReadNativeColumn`, in every reading method; a
conversion that fails returns its error as a failed scan does. `database/sql` reads every
named tuple as `map[string]any` and every address as `net.IP`, so it reads all of these as
they are, with two differences from the native driver: a wide integer that cannot be `NULL`
inside a named tuple is a `big.Int` value, which `encoding/json` writes as `{}`, and an
IPv4-mapped IPv6 address prints in its IPv4 form.

Some values clickhouse-go cannot read whatever the column is read into, with either driver,
so a column holding one is refused with [Q059](../diagnostics.md#q059) at its
select-list item:

- a `Map` keyed by, or mapping to, a 128- or 256-bit integer that cannot be `NULL`, at any
  depth, on which both drivers panic: make the values `Nullable`, as
  `CAST(m AS Map(String, Nullable(Int128)))` does, or select `mapKeys(m)` and
  `mapValues(m)`;
- a `Map` keyed by an `IPv4` or `IPv6` address, whose block the drivers cannot decode: key
  it by the address's text, as `CAST(m AS Map(String, String))` does;
- a `Map` keyed by an `Array`, a `Tuple` or a `Map`, whose Go types clickhouse-go cannot key
  a Go map by, or by a `Decimal`, whose type name it splits at the comma, so it cannot
  decode the block either: select `mapKeys(m)` and `mapValues(m)`. A `Map` keyed by a
  `DateTime64` with a time zone, as in `Map(DateTime64(3, 'UTC'), String)`, fails the same
  way, but Seagull keeps no time zone in the type and does not refuse it;
- an unnamed `Tuple` holding such an integer, which the native driver cannot read into a
  slice and `database/sql` fails on or reads as `NULL`: name its elements, as
  `CAST(x AS Tuple(a UInt8, b Int128))` does, or make the integer `Nullable`;
- a type nesting 64 or more levels deep, which is too deep to check that clickhouse-go
  can read it: select the nested part with `toJSONString(...)`, or its parts as columns
  of their own.

### JSON columns

ClickHouse returns a `JSON` value as the document clickhouse-go decodes, a `chcol.JSON`
holding each path's typed value, which the generated `JSON` type cannot scan for itself
everywhere: the native driver reads a `JSON` column only into a `chcol.JSON`, a
`chcol.JSONDeserializer`, a struct or a map, and both drivers hand a `JSON` value inside an
`Array`, a `Map` or a `Tuple` in their own forms. Both emitters read every such document
through one conversion in `clickhouse_documents.go`, so the same stored value reads as the
same bytes from either (see [JSON values](../type-mapping.md#json-values) for how those
bytes compare with the document written):

| Column | `driver: clickhouse` | `database/sql` |
|---|---|---|
| `JSON`, `Nullable(JSON)` | through `sglScanNativeComposite`, which is a `chcol.JSONDeserializer`, in a plain `SELECT`, a compound one, an embed, a runtime builder and a stream, whatever the column's Go type | as it is, through the `JSON` type's `Scan` |
| `Array(JSON)`, `Array(Nullable(JSON))`, `Map(K, Nullable(JSON))`, `Map(K, Array(JSON))`, a `Map` or `Array` of named tuples holding `JSON` | through `sglScanNativeComposite` | through `sglScanDriverComposite` |
| a `Tuple` holding `JSON`, named or not, and an `Array` or `Map` of tuples without names holding it | a tuple scanned into a `map[string]any` or a `[]any`, an `Array` of them into slices of `[]any`, and converted after the scan, a named tuple into its struct; a `Map` of them through `sglScanNativeComposite` | through `sglScanDriverComposite` |
| `Map(K, JSON)` | scanned entry by entry into `sglNativeEntries` and converted after the scan | refused with [Q059](../diagnostics.md#q059) |

A `NULL` reads as a nil `JSON`, and a `JSON` value an interface holds, such as an element
of a tuple's `map[string]any` behind `database/sql` or of a tuple's `[]any`, reads as the
package's `JSON` type. clickhouse-go v2.48 panics building any Go map of
`Map(K, JSON)` values, so `database/sql` cannot read such a column, and neither driver can
read one inside an `Array`, a `Map` or a `Tuple`, as in `Array(Map(String, JSON))`: both are
refused with [Q059](../diagnostics.md#q059); cast the `Map` so its values are
`Nullable`, as `CAST(m AS Map(String, Nullable(JSON)))` does, which both drivers read. A
`JSON` path the server inferred as a date reads as a Go time, and, inside a stored `Map`,
clickhouse-go v2.48 returns it as a number of days. With the connection setting
`output_format_native_write_json_as_string = 1` the server sends a `JSON` column as its own
text, which both emitters read as it is, but clickhouse-go v2.48 then fails or panics
reading a `JSON` value inside an `Array`, a `Map` or a `Tuple`.

### Appending to a native batch

Native `copyfrom`, and `batch` with lone placeholders, append typed rows through
PrepareBatch. Generated adapters convert maps of IP addresses or named tuples, nested
maps and structs, and supported override types into the Go values clickhouse-go accepts.
They validate every IP address before sending the batch. Named `exec` parameters use
text formatting instead.

The driver must decode the INSERT's column types before appending. A parameter holding a
map keyed by an IP address, Array, Tuple, Map or Decimal is refused
([Q059](../diagnostics.md#q059)). Use `exec` to bind that map by name as text.
[Override contracts](../type-mapping.md#what-an-override-type-must-support) describe the
supported user-defined types; an adapter does not make arbitrary driver types usable.

### Nested columns

ClickHouse stores a `Nested(colony String, minutes UInt16)` column `legs` as one array
column per field, `legs.colony Array(String)` and `legs.minutes Array(UInt16)`, as it
does with `flatten_nested` on, its default. The catalogue models what the server stores:

- `SELECT *` returns `legs.colony` and `legs.minutes`, and an `INSERT` names them, as
  `` `legs.colony` `` or `legs.colony`.
- `legs.colony` reads the field's array, and `legs` itself the array of tuples
  `Array(Tuple(colony String, minutes UInt16))`; `legs` is a hidden column, which a query
  may name but `SELECT *` leaves out.
- `ALTER TABLE ... ADD COLUMN legs.altitude Array(UInt32)`, `RENAME COLUMN`,
  `MODIFY COLUMN` and `DROP COLUMN` read a field's dotted name, and `DROP COLUMN legs`
  drops every field. A field added later is not part of the tuples `legs` reads as.

A table created with `flatten_nested` off keeps `legs` as one column, which Seagull does
not model.

### Grouping modifiers and `group_by_use_nulls`

`GROUP BY` accepts `ROLLUP (...)`, `CUBE (...)`, `GROUPING SETS (...)`, the
`WITH ROLLUP`, `WITH CUBE` and `WITH TOTALS` extensions, and `grouping(...)`. A subtotal
row leaves out some of the keys, and the `group_by_use_nulls` setting decides what such a
key holds. With the server's default, `0`, it holds its type's default value, such as an
empty string or `0`, so the key keeps its own nullability. A query that sets the option
itself is analysed with its own setting, and every key the grouping can leave out may
then be `NULL`:

```sql
-- sgl.query(RevenueByRegion, many)
SELECT region, sum(amount) AS revenue FROM sales
GROUP BY region WITH ROLLUP
SETTINGS group_by_use_nulls = 1;
```

Here `Region` is a `*string`, `nil` on the grand total row. When the server, the user's
profile or the connection sets `group_by_use_nulls = 1` for every query, set
`group_by_use_nulls: true` under the database's `dialect_options`, or pass
`dialect_engine_clickhouse.WithGroupByUseNulls(true)` to `dialect_engine_clickhouse.New` in Go.

## Migration semantics

ClickHouse has no DDL transactions and no advisory locks. The Seagull
migration backend (`migrate_backend_clickhouse.New()`) handles both
constraints:

- **Per-statement execution.** Seagull splits each migration's body on
  top-level semicolons and executes one statement at a time.
- **Append-only history.** Each up-migration records a single
  completed row on success. It does not use per-statement dirty or
  `last_statement` resumable tracking, so a failed migration does
  not record partial progress to resume from.
- **No-op locking.** ClickHouse migration locking is a no-op. There
  is no `sgl_migration_lock` table and no in-database serialisation
  between concurrent migrators. Coordinate concurrent migrators
  externally (CI lock or deployment gate) for production
  multi-replica setups.
- **History table engine.** The `sgl_migrations` and `sgl_seeds`
  tables use `ENGINE = ReplacingMergeTree(applied_at) ORDER BY
  version`, which collapses duplicate version rows on merge. Seagull
  reads the history with `FINAL` to see the current applied set.

## Asynchronous mutations

`ALTER TABLE ... UPDATE` and `ALTER TABLE ... DELETE` run
fire-and-forget on the ClickHouse server. The server accepts the statement,
queues a background mutation in `system.mutations`, and sends the
client no rows-affected count. Use the `asyncexec` command
to surface that lifecycle in the generated method:

```sql
-- sgl.query(PurgeOld, asyncexec)
ALTER TABLE events DELETE WHERE ts < {cutoff:DateTime};
```

The generated method has signature `PurgeOld(ctx, cutoff) error` and
carries a doc comment documenting that a nil return means the server
accepted the queued mutation, not that the mutation has finished
applying. Inspect `system.mutations` for completion status.

Using `exec` on the same statement still works because the underlying
driver call is identical. The analyser emits a Hint diagnostic
(`Q040`) recommending `asyncexec`. That diagnostic keeps a
synchronous-looking call signature from quietly hiding the
fire-and-forget semantics.

To wait for a mutation instead, use ClickHouse's `mutations_sync`
setting: with `2`, the statement returns only once every replica has
applied the mutation, and with `1`, once the server that took it has.
Set it on the connection (`clickhouse.Options.Settings`, or
`mutations_sync=2` in the DSN), or on one statement with
`SETTINGS mutations_sync = 2` after the `WHERE` clause. The analyser
cannot see the setting, so it still reports `Q040` on an `exec`. A
migration rollback on ClickHouse deletes its history row this way.

The native driver's `Exec` returns only an error, so the native
emitter refuses `execrows` and `execresult`.

## User-defined functions

`CREATE FUNCTION name AS (params) -> expr` registers a lambda in the
catalogue. A lambda reads no table and changes no data, so a query that calls it is
read-only and runs on the reader. A call is typed as ClickHouse types it, from the
lambda's body with each parameter taking the type of its argument:

```sql
CREATE FUNCTION times_two AS x -> x * 2;
CREATE FUNCTION add_xy AS (x, y) -> x + y;
```

```sql
-- sgl.query(Compute, one)
SELECT times_two(21) AS small, times_two({n:UInt32}) AS doubled, add_xy({a:Int32}, {b:Int32}) AS total;
```

`small` is a `uint16`, since `21` is a `UInt8` and a `UInt8` times a `UInt8` is a
`UInt16`; `doubled` is a `uint64`, and `total` an `int64`. Where the body calls something
Seagull cannot type, the call's column is `any`; anchor it with a `sgl.column` directive.

A lambda whose body calls the function itself is refused at `CREATE FUNCTION` with a
syntax error, as ClickHouse refuses it. Functions that call each other are typed `any`.
Typing a call types each body once for each set of argument types, with the dialect's
types and parse limits, and gives up, typing the column `any`, past a fixed budget, so
deeply nested lambda functions cannot hold up generation.

A function created with `CREATE FUNCTION` is found only in the case it was created with,
as ClickHouse finds it: after `CREATE FUNCTION Doubled AS x -> x * 2`, a call of `doubled`
or `DOUBLED` is an unknown function, [Q005](../diagnostics.md#q005). Built-in functions
are still found in any case.

Seagull still accepts the legacy form `CREATE FUNCTION name` without `AS lambda`. The
catalogue records the name, without parameters, so a call of it is typed `any`, and a call
that passes arguments is also [Q005](../diagnostics.md#q005).

## How expressions are typed

Seagull applies ClickHouse-specific type rules to computed columns. Representative
rules are listed below; [known limitations](../limitations.md#clickhouse) records gaps:

- **Literals** take the smallest type that holds them: `1` is a `UInt8`, `300` a
  `UInt16`, `-1` an `Int8`, and `1.5`, `.5` or `1e3` a `Float64`. `DATE '2024-12-31'` is
  a `Date` and `TIMESTAMP '2024-01-01 00:00:00'` a `DateTime`, the two typed literals the
  server reads, in any case.
- **Arithmetic** follows ClickHouse's result rules rather than the common type of the
  operands: a `UInt8` plus a `UInt8` is a `UInt16`, any subtraction of integers is
  signed, a `Float64` times an integer is a `Float64`, and `/` gives a `Float64`
  unless a decimal is involved. The function forms `plus`, `minus`, `multiply`,
  `divide`, `intDiv`, `modulo`, `negate` and `abs` are typed the same way. A remainder,
  `%`, `modulo` or `moduloOrZero`, takes the sign of its dividend and the width of its
  divisor, the next width above it when the dividend is signed, since a negative remainder
  of a narrow divisor may not fit it: an `Int32` modulo an `Int16` is an `Int32`.
  `positiveModulo` (and `pmod`, `positive_modulo`) is never negative, so it is unsigned at
  the divisor's width, and `widthBucket` is unsigned at twice its bucket count's width, the
  count being unsigned, since the bucket above the last one is `count + 1`.
- **Dates and times** moved by a unit take the type the server gives them, whether the
  query writes `date_add(HOUR, 1, d)` (or `dateAdd`, `timestamp_add` and the `sub`
  spellings), `addHours(d, 1)` or `d + INTERVAL 1 HOUR`: a day, week, month, quarter or
  year keeps the type; an hour, minute or second moves a `Date` to a `DateTime` and a
  `Date32` to a `DateTime64(3)`; and a millisecond, microsecond or nanosecond moves a
  `DateTime` to a `DateTime64` of that precision and a `DateTime64` to the finer of the
  two precisions. A number added to a date or time counts its own units and keeps its
  type. `INTERVAL` takes every unit spelling ClickHouse does, in any case: the units from
  `NANOSECOND` to `YEAR`, their plurals, the `SQL_TSI_` forms and abbreviations such as
  `d`, `hh` and `mcs`, written `INTERVAL 1 DAY`, `INTERVAL '1 day'` or
  `INTERVAL '1' DAY`. An unknown unit, as in `INTERVAL 1 fortnight`, is
  [Q010](../diagnostics.md#q010).
- **NULL**: most functions and aggregates return `NULL` only when an argument may be
  `NULL`, so `lower(name)` over a `String` column is a `string`. `coalesce` and `ifNull`
  are `NULL` only when every argument may be, the `OrNull` functions, `nullIf` and
  `toNullable` may always return `NULL`, and `count`, `uniq` and `groupArray` never do.
- **Arrays**: `length`, `empty` and `notEmpty` work on arrays and maps, `arr[i]` and
  `map[key]` read the element type (an index out of range reads the default, not
  `NULL`, though a `Nullable` element may still be `NULL`), `groupArray` and `topK` build
  an array of their argument's type, and `arraySum` sums integers to a 64-bit integer of
  their sign. `[1, 2]` is one `Array(UInt8)` value.
- **Predicates**: a predicate function, such as `isNull`, `isNotNull`, `empty`, `has`,
  `like`, `startsWith`, `mapContains` or `isNaN`, returns a `UInt8`, 1 or 0, as the
  server returns it, so its field is a `uint8`; the `NULL` tests `isNull`, `isNotNull`,
  `isZeroOrNull` and `isNullable` are never `NULL`. `and`, `or`, `xor`, `not` and the
  `NOT` operator return a `Bool` when an argument is a `Bool`, and a `UInt8` otherwise.
  The operators are typed as the server types them too: a comparison, `LIKE` and
  `BETWEEN` are a `UInt8`, a `Nullable(UInt8)` when a value they compare may be `NULL`;
  `IN` is a `Nullable(UInt8)` only when the value it tests may be, whatever its list
  holds; `IS NULL`, `IS NOT NULL` and `EXISTS` are a `UInt8` that is never `NULL`; and
  `AND` and `OR` are a `Bool` when an operand is a `Bool`, a `UInt8` otherwise, and
  `Nullable` when an operand may be `NULL`. So `SELECT wing > 40 AS long_wing` is a
  `uint8` field and `span > 100` over a `Nullable` column a `*uint8`.
- **Lambdas**: a lambda passed to an array function reads the array's elements, so its
  body is typed from them. `arrayMap(t -> length(t), tags)` is an `Array(UInt64)`;
  `arrayFilter`, `arraySort` and `arrayFill` keep the array's type; `arrayFirst` and
  `arrayLast` return an element; `arrayCount`, `arrayFirstIndex` and `arrayLastIndex` are
  `UInt32`; `arrayExists` and `arrayAll` are `UInt8`; `arrayFold` has its accumulator's
  type; and `arraySum`, `arrayMin`, `arrayMax`, `arrayAvg` and `arrayCumSum` are typed from
  the lambda's result. The result is `NULL` only when the array may be, and an element of
  `arrayMap`'s result may be `NULL` when the lambda's body may be, so
  `arrayMap(t -> nullIf(t, ''), tags)` is an `Array(Nullable(String))`. A lambda's
  parameter shadows a `WITH` expression of the same name inside the lambda's body, so in
  `WITH 5 AS x SELECT ... WHERE arrayExists(x -> x > 1, tags)` the body's `x` is the
  array's element.
- **Aliases**: a function alias, such as `replace` for `replaceAll`, `date_diff` for
  `dateDiff`, `YEAR` for `toYear`, `any_value` for `any` or `medianExact` for
  `quantileExact`, is typed as the function it names, with its combinators, as ClickHouse
  lists its aliases in `system.functions`.
- **Time buckets**: `toStartOfInterval(t, INTERVAL 15 MINUTE)` (and its aliases
  `time_bucket` and `date_bin`) is a `Date` for a unit of a week or longer, a
  `DateTime64` of the finer of the value's and the unit's precision for a unit shorter
  than a second, and a `DateTime` otherwise, as the server gives it. The calendar keys
  (`toYYYYMM`, `toYYYYMMDD`), `toMonday`, `toLastDayOfMonth`, `toStartOfFiveMinutes`, the
  `toIntervalUnit` functions and `toDateTime` of a `DateTime64` or a `Date` are typed too.
- **Quantiles**: the exact quantiles pick a value of their argument's type, and the
  t-digest and timing quantiles (`quantileTDigest`, `quantileTiming`, their weighted and
  plural forms and the `median` aliases) are `Float32`, as the server returns them.
- **Window functions and aggregate states**: `rank()`, `row_number()` and
  `dense_rank()` are `UInt64`, an aggregate `OVER` a window has the aggregate's type,
  `count(DISTINCT x)` is a `UInt64`, and a `-Merge` combinator such as `uniqMerge` or
  `sumMerge` reads back the type of the aggregate the state was built by, including
  `AggregateFunction(count)`.

ClickHouse has no `RETURNING` clause: Seagull reports one on an `INSERT`, `UPDATE`,
`DELETE` or `ALTER TABLE` mutation with `Q052`.

## Complex types

ClickHouse's variant, dynamic, and aggregate-state types read as follows (verified
with clickhouse-go v2.48 on ClickHouse 25.4):

| ClickHouse type | Generated Go type |
|---|---|
| `SimpleAggregateFunction(f, T)` | the Go type of `T`, as `uint64` for `SimpleAggregateFunction(sum, UInt64)` and `*string` for `SimpleAggregateFunction(max, Nullable(String))`: the column holds plain values of `T`, which both drivers read and bind as `T` |
| `Dynamic`, and a path of a `JSON` column such as `data.a.b` | `any`, holding the value of the type the row stores, or `nil`; natively the generated code reads it through `sglScanNativeComposite`, since the native driver scans such a value into no interface value, and behind `database/sql` the `any` holds the driver's `chcol.Variant`, which encodes to JSON as its value; an `Array(Dynamic)` is a `[]any` |
| `AggregateFunction(f, T)` | refused with [Q059](../diagnostics.md#q059) wherever a result holds one: neither driver decodes the state. Read its value with `finalizeAggregation(x)`, typed as the aggregate's result (`UInt64` for a `uniq` state), or the aggregate's `-Merge` combinator, as `uniqMerge(x)` |
| `Variant(A, B, ...)` | refused with Q059 wherever a result holds one: both drivers read a `Variant` only into their own `chcol.Variant`. Select the value as text with `toString(x)`, and its member's type name with `variantType(x)`; `variantElement(x, 'T')` is typed `any` for a `Variant` of several members, since Seagull cannot read the type name it is given |

A `Tuple` or `Map` is never `Nullable` itself: `tuple(note, id)` over a
`Nullable(String)` column `note` is a `Tuple(Nullable(String), UInt64)`, whose first field
is a pointer, and `map('note', note)` a `Map(String, Nullable(String))`.

Override the default per call site with `sgl.column(go_type: ...)`
when you have a type the driver can scan:

```sql
-- sgl.query(LoadEvent, one)
-- sgl.column(payload, go_type: "myapp.EventPayload")
SELECT id, payload FROM events WHERE id = {id:UInt64};
```

## Not supported yet

Seagull does not support these ClickHouse features:

- Cluster-wide DDL (`ON CLUSTER cluster_name`) parses through the
  clause as opaque metadata but offers no cluster-aware codegen.
- The `COLUMNS(...)` matcher fails with a syntax error asking for the
  columns to be named, since Seagull cannot list the columns it matches.
- A query that opens with `FROM` (`FROM t SELECT ...`) or with a parenthesis
  (`(SELECT ...) UNION ...`) fails with a syntax error; write `SELECT` first, without the
  outer parentheses.
- `runningDifference` and `neighbor` are not in the catalogue
  ([Q005](../diagnostics.md#q005)): both are deprecated and off by default since
  ClickHouse 25.4. Use a window function such as `lagInFrame`.

As in every dialect, a statement ClickHouse does not have and a `WHERE`, `PREWHERE`,
`HAVING`, `QUALIFY` or `ON` condition that is empty, unbalanced, ends in an operator or
leaves a word over are reported as [Q010](../diagnostics.md#q010). So are an
unbalanced parenthesis or bracket, in a query or in DDL, an empty select item, as in
`SELECT , 1` or `SELECT 1,, 2`, an `AS` without its alias, and a malformed scalar,
`EXISTS` or `IN` subquery. A trailing comma before `FROM`, as in `SELECT a, b, FROM t`, is
accepted, as ClickHouse accepts it, while a column named `from` after a comma still reads
as a column, as in `SELECT a, from FROM t`. Statements that hold no query, such as
`SHOW`, `SYSTEM` or `OPTIMIZE`, generate with no output.

Replay refuses what ClickHouse refuses: creating a table under a name a table already
holds is [Q067](../diagnostics.md#q067), and altering a table, or dropping a table,
view or function, that does not exist, without `IF EXISTS`, is
[Q068](../diagnostics.md#q068).

## Parse limits

`max_parse_depth` (256 by default, at most 4,096) and `max_tokens_per_statement` (100,000
by default, at most 1,000,000) under
[`dialect_options`](../configuration.md#dialect-options-dialect_options), or
`dialect_engine_clickhouse.WithMaxParseDepth` and `WithMaxTokensPerStatement` in Go, bound what one
statement may cost. The depth counts across scalar subqueries, the parentheses around a
subquery included, so it bounds the whole statement. A statement is tokenised only as far
as the token budget, and one past it fails when it is applied as DDL or read as a query,
even a `SET` or `SHOW` that Seagull does not analyse, while a large seed `INSERT` in a
migration is still passed over. Such a statement is still read to its end for a missing
semicolon, so a statement it runs on into is reported ([Q010](../diagnostics.md#q010))
at that statement's first word. `max_type_parse_depth`
(`dialect_engine_clickhouse.WithMaxTypeParseDepth`) caps how deeply a type name such as
`Array(Nullable(...))` nests; its default, 64, is also its ceiling. Each limit fails the
statement with an error matching `seagull.ErrLimit`. The Go options hold a value above the
ceiling at the ceiling; `sgl.yaml` and `dialect_registry.New` refuse it.

## See also

- [Directives](../directives.md), the full directive surface and
  command kinds.
- [Type mapping](../type-mapping.md) for the decimal and wide-integer
  types the ClickHouse drivers scan into, and [what an override type must
  support](../type-mapping.md#what-an-override-type-must-support) on either
  driver.
- ClickHouse documentation: https://clickhouse.com/docs
- `clickhouse-go/v2` driver: https://github.com/ClickHouse/clickhouse-go

## Exported declarations

### dialect_engine_clickhouse

```go
const Family = clickhouse.Family
func New(options ...Option) dialect.Dialect
type Option = clickhouse.Option
    func WithGroupByUseNulls(enabled bool) Option
    func WithJoinUseNulls(enabled bool) Option
    func WithMaxParseDepth(depth int) Option
    func WithMaxTokensPerStatement(limit int) Option
    func WithMaxTypeParseDepth(depth int) Option
    func WithName(name string) Option
```
