---
title: Type mapping
description: "How SQL types and nullability map to Go types, including overrides and driver differences."
section: Reference
subsection: Core
order: 30
---

# Type mapping

Every result column and parameter has an inferred SQL type and nullability. Seagull maps
that pair to a Go type: the "not null" Go type when the value can never be NULL, and the
"nullable" Go type otherwise.

## Defaults

Each dialect describes the structure of its types: width, signedness, temporal kind and
other attributes. The mapping uses those attributes, so PostgreSQL `bigint` and
ClickHouse `Int64` both become `int64` without relying on their spellings.

| SQL type, by structure | Not null | Nullable |
|---|---|---|
| Signed integer of 8, 16, 32 or 64 bits (24-bit `MEDIUMINT` widens to 32) | `int8`, `int16`, `int32`, `int64` | pointer |
| Unsigned integer of 8, 16, 32 or 64 bits (MySQL `UNSIGNED`, ClickHouse `UInt*`, DuckDB `U*`) | `uint8`, `uint16`, `uint32`, `uint64` | pointer |
| Integer wider than 64 bits (`Int128`, `UInt256`, `HUGEINT`, ...) | `string` (BigInt slot) | `*string` |
| Integer whose width the dialect does not know | `int64` | `*int64` |
| 32-bit float (`real`, MySQL and DuckDB `FLOAT`, MySQL `FLOAT(p)` up to 24 bits and `FLOAT4`, `Float32`) | `float32` | `*float32` |
| 64-bit float (`double precision`, `DOUBLE`, MySQL `FLOAT(p)` of 25 to 53 bits and `FLOAT8`, `Float64`, SQLite `REAL` and `FLOAT`) | `float64` | `*float64` |
| Exact decimal (`numeric`, `decimal`) | `apd.Decimal` (Decimal slot) | `*apd.Decimal` |
| Money (`money`) | `string` (Money slot): the text the engine writes, with its currency symbol and group separators, which no decimal type reads | `*string` |
| Boolean, and a comparison on every engine but ClickHouse | `bool` | `*bool` |
| Text of any kind, a bit string (`bit`, `varbit`) included | `string` | `*string` |
| Binary (`bytea`, `blob`) | `[]byte` | `[]byte` |
| Date or timestamp, with or without a time zone | `time.Time` | `*time.Time` |
| Time of day (`time`) | `time.Time` (TimeOfDay slot) | `*time.Time` |
| Time of day with a time zone (`timetz`) | `string` (TimeOfDay slot when it is set) | `*string` |
| Interval | `string` (Interval slot) | `*string` |
| JSON | `JSON` (emitted in `sgl_json.go`, JSON slot) | `JSON` |
| UUID | `uuid.UUID` | `*uuid.UUID` |
| Single IP address (ClickHouse `IPv4`, `IPv6`) | `netip.Addr` (IPAddress slot) | `*netip.Addr` |
| Network with a prefix, hardware address (`inet`, `cidr`, `macaddr`) | `any` | `any` |
| Enum | a string type declared in `models.go`, with a constant per value (Enum slot) | a pointer to it |
| Array | a slice of the element's Go type, a pointer where the element may be NULL (see [elements that may be NULL](#elements-that-may-be-null)) | the same |
| Map (ClickHouse `Map(K, V)`, DuckDB `MAP(K, V)`) | `map[K]V` of the key's and value's Go types, the value a pointer where it may be NULL (Map slot) | the same |
| Struct of named fields (ClickHouse named `Tuple`, DuckDB `STRUCT`) | a struct type declared in `models.go`, with a field of each field's Go type tagged with its SQL name (Struct slot) | a pointer to it |
| PostgreSQL composite type (`CREATE TYPE ... AS (...)`) | `any`; a field selected from it, as `(address).street`, maps as the field's type | `any` |
| Tuple with positional fields (ClickHouse `Tuple(T, U)`, `tuple(a, b)`, `(a, b)`) | `[]any`, holding the fields in order | the same |
| Union, aggregate state | `JSON` for a union, `[]byte` for an aggregate state | the same |

Every SQLite integer spelling (`INTEGER`, `INT`, `SMALLINT`, a foreign key, a rowid alias)
is a signed 64-bit integer, because SQLite stores every integer in up to eight bytes, so
each maps to `int64`. To keep `int32` for ordinary columns, override the `integer` type:
SQLite names the rowid alias, the integers of a `STRICT` table, counts and sums `bigint`,
so they stay `int64`:

```yaml
go:
  types:
    overrides:
      - sql: integer
        go: int32
```

SQLite types a column by the type it is declared with, as both of its drivers read it.
`REAL`, `FLOAT` and `DOUBLE` are 64-bit floats. `DATE`, `DATETIME` and `TIMESTAMP`, declared
exactly so, are `time.Time`, which the drivers read them into; with a modifier, such as
`TIMESTAMP(3)`, the drivers return the value as it is stored, so the column is a `string`.
A `UUID` column is a `uuid.UUID`, which reads a UUID's text or its 16 bytes. `TIME`,
`TIMETZ`, `TIMESTAMPTZ`, `INTERVAL`, `INET`, `CIDR`, `MACADDR` and `XML` columns hold the
text written into them, so each is a `string`, and `BYTEA`, `BINARY` and `VARBINARY` are
`[]byte`. Any other spelling takes the type of its affinity, so `MEDIUMTEXT` is text and
`LONGBLOB` binary. A
`CAST` converts by the affinity of the type it names, so `CAST(x AS DATETIME)`,
`CAST(x AS UUID)` and `CAST(x AS BOOLEAN)` are numbers, `numeric` and so `apd.Decimal`;
cast to `TEXT` to keep a value's text.

ClickHouse has no boolean result for a test: a comparison, `BETWEEN`, `IN`, `IS NULL` or
`EXISTS` is a `UInt8`, 1 or 0, so it reads into a `uint8`; `AND`, `OR` and `NOT` give a
`Bool` only when an operand is one.

### Decimals and enums

`apd.Decimal` is [`github.com/cockroachdb/apd/v3`](https://github.com/cockroachdb/apd), an
arbitrary-precision decimal implementing Scanner and Valuer. Generated methods adapt
nullable decimal pointers for drivers that cannot bind them directly. A runtime
builder's `Where` passes values to the driver as given: pass a decimal by value.
`uuid.UUID` comes from [`github.com/google/uuid`](https://github.com/google/uuid).

Each enum becomes a named string type in `models.go`, with a constant for each value,
written as the type's name followed by the value in Pascal case, and a `Valid` method that
reports whether a value is one of them. An enum created with `CREATE TYPE` (PostgreSQL,
DuckDB) is named after the type, so `mood` gives `Mood`, `MoodHappy` and `MoodSad`, with
the schema's name first when the schema is off the search path (`audit.mood` gives
`AuditMood`). One written inline in a column (MySQL and MariaDB `ENUM(...)`, ClickHouse
`Enum8` and `Enum16`) is named after the first table and column that hold it, in the
order the migrations declare the tables, such as `UsersStatus` for `users.status`, or
after the query and the column or parameter when no table does, such as
`ListTicketsPriority`; every column with the same values shares it, and a table a later
migration adds never renames it. A
struct of named fields becomes a struct type the same way, named after its first owner. To keep enums as plain strings, set the Enum slot
to `string` (`types.enum` in `sgl.yaml`); to use a type of your own, name it with a
`go_type:` column directive or a column override (see below).

Generated enums bind and read through their underlying strings. Adapters convert them
where a driver requires plain string values:

| Emitter | Converted enum values |
|---|---|
| pgx | Array parameters, slice parameters and array results use strings, including pointer elements. |
| PostgreSQL database/sql | Array parameters use strings; array results use JSON. Slice inputs expand into individual values. |
| Native ClickHouse | Scalar and map results, plus supported native batch values, use strings. Arrays and tuple fields can scan directly. |
| ClickHouse database/sql | Array and map results use strings before conversion to the generated enum type. |

Unsupported deeper combinations are [Q059](diagnostics.md#q059), including maps keyed
by enums and some nested ClickHouse enum values. Cast the result, change the parameter's
SQL type, or set `types.enum: string`. pgx also refuses COPY into enum arrays and
multi-dimensional enum arrays; see [PostgreSQL drivers](dialects/postgres.md#drivers).

Some drivers only scan into particular types, so the emitter adjusts the defaults for
them through its type slots:

| Dialect and driver | Decimals | Wide integers | Intervals | Time of day | IP addresses | Named tuples and structs | Maps |
|---|---|---|---|---|---|---|---|
| PostgreSQL family, `driver: pgx` | `apd.Decimal` | `string` | `time.Duration` | `time.Time`, `string` for `timetz` | `netip.Addr` | struct | `map[K]V` |
| PostgreSQL family, `database/sql` | `apd.Decimal` | `string` | `string` | `string` | `netip.Addr` | struct | `map[K]V` |
| MySQL family, `database/sql` | `apd.Decimal` | `string` | `string` | `string` | `netip.Addr` | struct | `map[K]V` |
| ClickHouse, `driver: clickhouse` | `github.com/shopspring/decimal` `Decimal` | `*big.Int` | `string` | `time.Time` | `netip.Addr` | struct | `map[K]V` |
| ClickHouse, `database/sql` | `github.com/shopspring/decimal` `Decimal` | `*big.Int` | `string` | `time.Time` | `net.IP`, `*net.IP` when nullable | `map[string]any` | `map[K]V` |
| DuckDB (`database/sql`) | `apd.Decimal` | `*big.Int` | `string` | `time.Time`, `TIMETZ` included | `netip.Addr` | struct | `map[K]V` |

The database/sql emitter uses the first recognised module in `Info.Drivers`;
`emit_driver_databasesql.WithDriver` pins another profile. Profiles account for driver representations,
including ClickHouse's decimal, wide integer, address and named-tuple types.

DuckDB returns its own values for composites, decimals and intervals, and decodes JSON.
Generated queries cast plain decimal, interval and JSON results to VARCHAR for reading,
and adapt nested list, struct and map values into the selected Go types. See
[DuckDB drivers](dialects/duckdb.md#drivers) for paths that must use decoded values instead.

pgx decodes a value of a type it has a codec for from its binary form, and reads it only
into the types that codec scans: a bit string (`bit`, `varbit`) into its own `Bits`, and an
interval into a `time.Duration` or its own `Interval`. So a `driver: pgx` package reads a
bit string column, and an interval column whose Go type is a string (an Interval slot of
`string`), through a cast to `text` the generated query writes, `CAST(flags AS text)`, as
`database/sql` reads them; a column it cannot wrap, such as one of a compound query, is
read as it is. A `timetz`, which pgx has no codec for, arrives as its text. Both emitters
therefore read `money`, `timetz`, `bit` and `varbit` into the same `string`.

### JSON values

The generated `JSON` type in `sgl_json.go` holds a document's bytes. Its Value writes nil
as NULL and other values as text, preserving those bytes. Binding text avoids drivers
treating JSON bytes as binary data. Scan accepts text, bytes or a decoded driver value;
a decoded value must be encoded again.

| Engine, emitter | What reaches the driver | What the engine stores and returns |
|---|---|---|
| PostgreSQL, TimescaleDB, `database/sql` and `driver: pgx` | the text, through `JSON`'s `Value`, which pgx calls for a parameter, an array element and a `copyfrom` row, in every query mode | `json` keeps the text byte for byte; `jsonb` stores it normalised: keys sorted, spacing rewritten, numbers as written |
| CockroachDB, `database/sql` and `driver: pgx` | the text | `JSONB`, normalised as PostgreSQL's `jsonb`; CockroachDB has no arrays of JSON |
| MySQL, `database/sql` | the text | a binary document, normalised: keys sorted, spacing rewritten, `2.50` as `2.5` |
| MariaDB, `database/sql` | the text | text (`JSON` is `LONGTEXT` checked by `JSON_VALID`), byte for byte |
| SQLite, `database/sql` (`modernc.org/sqlite`, `mattn/go-sqlite3`) | the text | `TEXT` (`typeof` is `text`), byte for byte |
| DuckDB, `database/sql` | the text, through `sglDriverValue`; a list of JSON values, whose elements the driver would otherwise panic on, through `sglDriverComposite`, each element as its text | `JSON` text, byte for byte; a `JSON` column is read through a cast to `VARCHAR`, but one inside a `LIST` or `STRUCT` reads back as the driver's decoded value encoded again |
| ClickHouse, `database/sql` and `driver: clickhouse` | the text, written into the statement by `sglClickHouseValue`; a native batch or `copyfrom` appends the document's bytes, which the driver writes as text | the `JSON` type, normalised: keys sorted, spacing removed, numbers typed, and returned as the document clickhouse-go decodes, which both emitters read as the same text (below); a nil `JSON` is refused by a `JSON` column and stored as `NULL` in a `Nullable(JSON)` one |

A document read back from an engine that normalises it, or encoded again from a decoded
value, is the same JSON but not the same bytes; one encoded again in Go also escapes `<`,
`>` and `&` as `\u003c`, `\u003e` and `\u0026`. Comparing a JSON column with a document
follows the engine: PostgreSQL's `json` has no equality, so compare `jsonb` or cast to
`text`; MySQL compares a JSON column with a string as a JSON string, so compare with
`CAST(? AS JSON)`. A type you name in the JSON slot or a column override binds as its own
`Value` says, except that on SQLite and DuckDB an override's `[]byte` type binds as text
(see [What an override type must support](#what-an-override-type-must-support)); a JSON
slot type should write its document as a `string` from `Value`, as the generated type does.
Code generated before the type wrote text stored each SQLite document as a `BLOB`, which
no longer equals a document bound as text; `UPDATE documents SET data = CAST(data AS TEXT)
WHERE typeof(data) = 'blob'` converts such rows.

Both ClickHouse emitters convert decoded documents to JSON text, including inside
supported composites. The bytes can differ from the input: key order and spacing change,
`2.50` becomes `2.5`, date-like paths can become Go time strings, and null or empty-object
paths can disappear. Go encoding also escapes `<`, `>` and `&`. `toJSONString(column)`
returns the server's own text, which can quote large integers.
See [ClickHouse JSON columns](dialects/clickhouse.md#json-columns) for driver restrictions.

The `database/sql` emitter reads a PostgreSQL or CockroachDB array through the dialect's
to-JSON function, which writes some elements in the engine's own form rather than the one
Go's JSON decoder takes; the generated `sglScanInto` reads those too: a timestamp without
a zone, as UTC, and a date into a `time.Time`, a number into a string or a type that
unmarshals itself from text, such as `apd.Decimal`, and binary data written as `\x` and hex
digits into a `[]byte`. So `timestamp[]`, `date[]`, `numeric[]` and `bytea[]` columns read on
every emitter.

`apd.Decimal`'s text and JSON marshalling methods have pointer receivers. A row struct that
holds an `apd.Decimal` by value loses the number when passed to `json.Marshal` by value, so
marshal a pointer to the row (`json.Marshal(&row)`) or override the type.

## Changing a type

Seagull applies the first rule that matches, in this order:

1. **A `go_type:` column directive** on one query, or in a migration for every query that
   passes the column through:

   ```sql
   -- sgl.query(GetSettings, one)
   -- sgl.column(settings, go_type: "example.com/app/settings.Settings")
   SELECT settings FROM users WHERE id = $1;
   ```

   Name an exported type, qualified by its import path unless it is built-in or belongs
   to the generated package. Pointer and slice prefixes and generic type arguments are
   accepted, for example `*example.com/app/money.Pence` or
   `github.com/x/opt.Option[uuid.UUID]`. Anonymous composites such as `map[string]int`
   are refused; declare a named type. An unusable value is
   [Q049](diagnostics.md#q049).

   The import name is inferred from the path. Set `go.types.package_names`
   (`seagull.WithPackageNames`) when the package declaration uses another name.
   Types in the generated package can use their bare name or package import path;
   `sgl generate` obtains that path from the nearest `go.mod`, while embedded callers
   supply `Input.PackagePath`.

   On an array column a type that is not a slice names the elements' type, and the column
   becomes a slice of it, holding pointers where the elements may be NULL, as an array's
   default Go type does: a `uuid[]` column overridden as `github.com/google/uuid.UUID` is a
   `[]*uuid.UUID` in the model, in a query's result and in a parameter written to or
   compared with the column, and a `[]uuid.UUID` once its elements are declared non-null
   (see [elements that may be NULL](#elements-that-may-be-null)). A slice
   type, such as `[]*string`, names the whole value. A nullable array stays a slice, nil
   for NULL.

2. **A configured override** for one column (`table.column` or `schema.table.column`),
   then for a SQL type. From Go, pass `seagull.WithTypeOverrides` a `seagull.TypeOverride`
   with `Column` or `SQLTypeName` set. A column override acts like a migration `go_type:`
   directive on that column, so a directive on the same column still wins:

   ```yaml
   go:
     types:
       overrides:
         - sql: uuid
           go: github.com/gofrs/uuid/v5.UUID
         - column: users.settings
           go: example.com/app/settings.Settings
   ```

   A SQL type override names a built-in type such as `uuid` or `text`, or an enum or
   composite type created in a migration. An unqualified name such as `mood` finds the
   type through the schema search path; a qualified one such as `audit.mood` matches only
   that schema's type. A column of a domain takes the domain's base type, so an override
   for the base type applies to it.

   The `go` value follows the `go_type:` rules above, with two differences: name the value
   type, without `*`, since a nullable column becomes a pointer automatically, and keep `[]`
   to built-in element types such as `[]byte`. An unusable value is Q049, reported against
   `sgl.yaml`.

3. **A type slot** in `go.types`, which replaces a whole category:

   ```yaml
   go:
     types:
       decimal: string                       # no dependency, lossless text
   ```

   Slots exist for `decimal`, `money` (whose default is `string`, whatever `decimal`
   says), `bigint` (wide integers), `json`, `interval`, `time_of_day`, `ip_address` and
   `enum`. From Go they are the `Types` field of a `seagull.Preset`, passed with
   `seagull.WithPreset`, which also has `Map` and `Struct` slots for the composite types.

4. **The emitter's defaults**, such as the native ClickHouse decimal type or pgx's
   `time.Duration` intervals. An emitter supplies them by implementing
   `emit.TypeDefaulterPort`; see [Writing an emitter](../how-to/write-an-emitter.md).

5. **The structural defaults** above.

A SQL type override is the one rule that keys on an engine's type name, which is what it
is for: `sql: integer` changes exactly the types a dialect names `integer`.

`type:` in a column directive is different: it overrides the inferred SQL type, which
then maps through the rules above. Use it when inference is too loose, for example on a
derived expression.

### Where each override applies

SQL type overrides and slots apply to models, result rows and parameters. Column
overrides apply to the model field, direct results (including through views), and
parameters compared with or assigned to that column. For example, an override on
`orders.status` affects both `SELECT status` and `UPDATE orders SET status = $1`.

A cast to the column's own SQL type retains the override. A cast to a different type,
a function call or another computation produces a new value and uses that value's
mapping; a type with other modifiers is a different type, so `code::varchar(4)` of a
`varchar(8)` column, or `CAST(amount AS DECIMAL(12, 4))` of a `DECIMAL(10, 2)` one, drops
the override. Overrides do not pass through CTEs or derived tables; repeat the `go_type:`
directive on the query result where needed.

A parameter's SQL declaration can likewise retain the override when it restates the
column's type: `{host:IPv4}` keeps an IPv4 column's override, while `{host:String}` does
not. Equivalent SQL spellings count as the same type, and nullability does not affect
that comparison. Length, precision and scale distinguish declarations here too:
`$1::varchar(4)` does not restate a `varchar(8)` column, nor
`CAST(? AS DECIMAL(12, 4))` a `DECIMAL(10, 2)` one, so neither takes the column's
override, while a cast to the type with the column's own modifiers, or with none, does. On
DuckDB, `CAST(? AS ...)` keeps no modifiers for its parameter, so it restates the column
whatever modifiers it writes.

Pattern parameters, parameters compared with computed expressions, and parameters whose
`sgl.param` declares `type:` use their own mappings rather than the column override.

### What an override type must support

An override names a Go type of your own, which the generated code binds, writes in every
command (`exec`, `batch`, `copyfrom`) and scans, alone, as a pointer for a nullable column
and as the element of a slice for an array. Every emitter supports, on every one of those
paths:

- **A defined type over the column's own kind**: a type whose underlying type is the kind
  of the Go type the column maps to by default, such as `type Label string` or
  `html/template.HTML` for text, `type Count int64` for an integer (of any width that
  holds the values), `type Ratio float64` for a float and `type Flag bool` for a boolean;
  a `[]byte` type, such as `json.RawMessage`, for text, JSON and binary columns; and a
  string type, such as `encoding/json.Number`, for an IP address column, which holds the
  address as its text. This is `database/sql`'s own contract, which converts a driver's
  value into a defined type of the same kind and binds one as its kind.
- **A type the driver reads and writes for the column**, such as `netip.Addr` or `net.IP`
  for an address on pgx and the native ClickHouse driver, `uuid.UUID` where the driver
  knows it, or `time.Duration` for an interval on pgx.
- **A type that reads and writes itself**: one that implements `sql.Scanner` and
  `driver.Valuer`, which `database/sql` and the native ClickHouse driver both consult, or,
  on pgx, one pgx's type map scans and encodes, which includes those two interfaces.

Any other Go type is yours to make work: Seagull writes it into the generated code as
named and leaves the driver to bind and scan it. A defined type of another kind than the
column's, such as a string type for an integer column, is outside the contract.

Generated adapters cover these driver gaps for overridden values:

| Driver API | Adaptation |
|---|---|
| database/sql | Converts supported text, boolean, address and composite results into the override's representation. |
| database/sql on DuckDB | Converts defined types inside supported collections to the driver's basic Go kinds. |
| database/sql on SQLite and DuckDB | Binds byte-based text and JSON overrides as text. |
| Native ClickHouse | Converts supported overridden results and native batch values; tuple results must use a type the driver reads. |
| database/sql and pgx | Prefers BinaryMarshaler bytes for overridden binary parameters, including UUIDs. |

The binary rule keeps a UUID override on BINARY(16), BLOB or bytea as its 16 bytes rather
than its Valuer's 36-character text. Reading the bytes still needs Scanner or a compatible
byte representation; BinaryUnmarshaler alone is not used.

Code generated before the binary binding stored such a value as its text: SQLite kept the
36 characters in the `BLOB` column, and PostgreSQL their 36 bytes in a `bytea` one, so a
lookup by the 16 bytes misses those rows. Convert them once, as in SQLite 3.41 or later:
`UPDATE tokens SET id = unhex(replace(id, '-', '')) WHERE typeof(id) = 'text'`.

A named parameter, which every ClickHouse `exec`, query and multi-row `batch` binds as text
through `sglClickHouseValue`, writes a defined type by its kind on both ClickHouse emitters.
A `Tuple` column's override must be a type the native driver reads, since it reads a tuple
into a struct by its fields without consulting a `sql.Scanner`.

`netip.Addr` is a type the native ClickHouse driver and pgx read for an address, and one
that unmarshals itself from text, so it reads on every emitter: behind `database/sql`
from the text PostgreSQL returns for `inet` and from the `net.IP` ClickHouse returns for
`IPv4` and `IPv6`, and it binds as its text.

## Nullability

Seagull tracks nullability through expressions. An outer join can make a `NOT NULL`
column nullable, and a scalar subquery is nullable when it may return no row. Most
aggregates over no rows and `CASE` without `ELSE` are nullable too.

Ordinary comparisons propagate operand nullability. `IS NULL`, `EXISTS`, NULL-safe
comparisons and `count(*)` are non-null; `COALESCE` is non-null when it has a non-null
alternative. Use a column directive's `nullable:` only when you can assert whether the
result may be NULL at execution time.

### Elements that may be NULL

PostgreSQL-family arrays and DuckDB lists, arrays and map values can hold NULL elements
independently of the declaration. Results therefore generally use pointers:
`int4[]` becomes `[]*int32`, and `MAP(VARCHAR, INTEGER)` becomes `map[string]*int32`.
Map keys remain non-null. ClickHouse declares element nullability explicitly, so
`Array(Int32)` is `[]int32` and `Array(Nullable(Int32))` is `[]*int32`.

Expressions can prove elements non-null. Examples include `array_agg` of a non-null
column, arrays or lists of non-null literals, and `map_keys`. Their results use plain
elements without an override.

PostgreSQL array parameters normally allow NULL elements too, including parameters
cast as arrays. Quantified comparisons (`id = ANY($1)`) and `kind: slice` inputs use
plain elements. A parameter follows the compared or assigned column's element declaration
unless its own `non_null_elements:` overrides it.

DuckDB's driver cannot bind a NULL list element or map value, so parameters use plain
elements; declaring them nullable is [Q059](diagnostics.md#q059). Write such NULLs in
SQL, for example `[1, NULL]`, rather than passing them as parameter elements.

When the data never holds a NULL element, declare the elements non-null with the
`non_null_elements` quality, at whichever level fits:

| Level | How | Scope |
|---|---|---|
| Query column | `-- sgl.column(tags, non_null_elements: true)` in the query header | that output column |
| Parameter | `-- $1 as sgl.param(tags, non_null_elements: true)` in the query header | that parameter |
| Table column | `-- sgl.column(posts.tags, non_null_elements: true)` in a migration, or `{ column: posts.tags, non_null_elements: true }` under `go.types.overrides` (no `go` needed) | the column, in the model, in every query that returns it as its own type and in every parameter compared with it or written to it |
| SQL type | `{ sql: "text[]", non_null_elements: true }` under `go.types.overrides` | every value of the type |
| Generator | `go.types.non_null_elements: true` in `sgl.yaml`, or `Preset.NonNullElements` | every array, list and map |

The narrower level wins, and `non_null_elements: false` declares the elements nullable again
under a wider `true`. An element the analysis shows may be NULL stays a pointer whatever is
declared: one of `ARRAY['a', NULL]`, or of an `array_agg` of a nullable column. A NULL that
reaches an element declared non-null is an error when the row is read, never a zero value
in its place: pgx refuses it itself, and the `database/sql` helpers refuse it with
`converting NULL to int32 is unsupported`.

### Nil and empty slices

A nil Go slice passed for an array or list parameter binds `NULL`, and an empty non-nil
slice binds an empty array, on PostgreSQL, CockroachDB, TimescaleDB and DuckDB, with every
emitter: `UPDATE posts SET tags = $1` stores `NULL` for `nil` and `{}` for `[]string{}`.
ClickHouse has no `NULL` array, since an `Array` cannot be `Nullable`, so there a nil
slice binds an empty array. A `kind: slice` list is not an array parameter: it expands into
the `IN` list, where a nil and an empty slice both write the dialect's empty-list test (see
[slice parameters](../how-to/slice-parameters.md)).
