---
title: CockroachDB
description: 'Select the dialect with dialect: cockroachdb in sgl.yaml, or dialect_registry.New("cockroachdb",
  ...) from Go.'
section: Reference
subsection: Dialects
order: 130
---

# CockroachDB

Select the dialect with `dialect: cockroachdb` in `sgl.yaml`, or `dialect_registry.New("cockroachdb", ...)` from Go.

## Capability summary

| Concern | Contract |
|---|---|
| Parser family | PostgreSQL |
| Generated API | database/sql or pgx |
| Live introspection | Yes, PostgreSQL-family source |
| Runtime migrations | Non-transactional migrations; table locking by default. Verify the required CockroachDB version before deploying locks. |

[Known limitations](../limitations.md#cockroachdb) records inference gaps and workarounds; the
[driver matrix](../driver-matrix.md) compares generated APIs.

The [engine guide](../../how-to/cockroachdb.md) and [runnable examples](../../../examples/cockroachdb) cover execution. The declarations below link to constructor contracts. Parser limits affect analysis, not server execution.

## What Seagull understands

- PostgreSQL query and schema forms, with the CockroachDB differences below. Parser
  acceptance can exceed engine support; consult [known limitations](../limitations.md#cockroachdb).
- CockroachDB types such as `STRING`, `INT8` defaults and `unique_rowid()`.
- CockroachDB-specific functions registered on top of the PostgreSQL catalogue: among them
  `gen_random_ulid`, `unordered_unique_rowid`, `uuid_generate_v4`, `ulid_to_uuid`,
  `follower_read_timestamp`, the fuzzy matching functions `levenshtein`, `soundex`,
  `metaphone` and `difference`, and `array_cat_agg`. The SHA digests return hexadecimal
  `STRING`, `num_nulls` an `INT8` and `pg_sleep` a boolean, as CockroachDB has them.
- PostgreSQL functions CockroachDB 25.1 lacks or has not implemented, such as
  `regexp_count`, `regexp_match`, `pg_advisory_lock`, `pg_notify`, `date_bin`,
  `make_interval`, the interval justifications, the jsonpath queries, the range
  constructors, `date_add`, `date_subtract`, `trim_array`, the size functions such as
  `pg_database_size` and the transaction identifiers `txid_current` and
  `pg_current_xact_id`, are left out of the catalogue, so a call of one is an unknown function
  ([Q005](../diagnostics.md#q005)) rather than a query that fails on the server.

- `UPSERT`, analysed as an `INSERT` (target, columns, `VALUES` or `SELECT`, `RETURNING`
  and the target columns' types for its parameters), and recorded as replacing a
  conflicting row, as `ON CONFLICT DO UPDATE` does. CockroachDB takes no `ON CONFLICT` clause on an `UPSERT`,
  so the dialect reports one as [Q010](../diagnostics.md#q010).
- Partitioning with the table's own `PARTITION BY LIST` or `RANGE` list, which creates no
  table per partition. PostgreSQL's `PARTITION OF`, `ATTACH PARTITION` and
  `DETACH PARTITION` are syntax errors in CockroachDB, and the dialect reports them as
  [Q010](../diagnostics.md#q010).
- Whole-row references as CockroachDB has them: the row of any relation named where a
  value is expected, a table's included, is a `record`, mapped to `any`, and it is never
  `NULL`, because CockroachDB gives the outer side of a join that matched nothing a record
  of `NULL`s rather than `NULL`; `row_to_json(b)` of such a row is a JSON object of `null`s.

- The alias of a table function that returns one column names that column whatever the
  function calls it, as CockroachDB names it: `FROM jsonb_array_elements(doc) AS e` reads
  `e`, and `e.value` or `value` are unknown columns, where PostgreSQL reads `value`. A
  function called without an alias keeps its column's name. The same holds for a function
  with one `OUT` parameter, whose column CockroachDB names after the alias where
  PostgreSQL keeps the parameter's name.
- Functions as FROM items, `CALL` of a procedure with `OUT` parameters, the
  `json_populate_record` family, composite field selections, `ARRAY(SELECT ...)` and
  nested array literals read as on PostgreSQL (CockroachDB 25.1 accepts each), except that
  a function returning one row of a table's type that finds none returns no row on
  CockroachDB, where PostgreSQL returns a row of `NULL`s.
- `ROWS FROM (f(...), g(...))` reads as on PostgreSQL: each function's columns in turn,
  every one of them `NULL` where a shorter function is padded, then the ordinality column.
  The alias of a `ROWS FROM` of several functions names none of their columns on
  CockroachDB either. CockroachDB rejects a column definition list on a function inside
  `ROWS FROM`, which Seagull accepts.
- Integer division as CockroachDB does it: `/` between two integers is a `DECIMAL`, the
  exact quotient, and `//` is integer division. The difference of two dates is an `INT8`, as
  CockroachDB's `INT` is.
- `INDEX`, `UNIQUE INDEX`, `INVERTED INDEX`, `VECTOR INDEX` and `FAMILY` elements inside
  `CREATE TABLE` declare an index or a column family, not a column; a `UNIQUE INDEX`
  also makes its columns unique. A primary key or unique constraint may be hash-sharded
  (`PRIMARY KEY (ts, id) USING HASH`) and store columns (`STORING (...)`). A hash-sharded primary key gets the hidden
  column CockroachDB adds to hold each row's shard, such as `crdb_internal_ts_id_shard_16`
  (the bucket count of `WITH (bucket_count = n)`, 16 by default): it leads the primary key,
  and `SELECT *` leaves it out. The shard column of a hash-sharded secondary index is not
  modelled.
- Computed columns written `AS (expression) STORED` or `VIRTUAL`, CockroachDB's form of
  `GENERATED ALWAYS AS`, which an `INSERT` leaves out.
- A `NOT VISIBLE` column is left out of `SELECT *` and `t.*`, as CockroachDB leaves it out,
  while a query may still name it.
- A table created without a primary key gets the hidden `rowid` column CockroachDB adds,
  an `INT8` with a default that keys the table: `SELECT *` leaves it out and a query may
  name it. A table that declares a `rowid` column of its own gets `rowid_1`, or the next
  free `rowid_n`, as CockroachDB names it. `ALTER TABLE ... ADD PRIMARY KEY` and
  `ALTER TABLE ... ALTER PRIMARY KEY USING COLUMNS (...)` give the table the new key and
  drop the hidden column, as CockroachDB drops it. Introspection reads the same hidden
  column and key.
- A sequence can be read as a relation with the columns `last_value`, `log_cnt` and
  `is_called`, as on PostgreSQL. Names qualified by the database (`app.public.accounts`)
  and `SET search_path` in a migration replay as on PostgreSQL; a two-part name is read as
  `schema.table`, where CockroachDB may read it as `database.table`, and `USE` is not
  followed.
- `AS OF SYSTEM TIME` after a query's FROM clause, the historical and follower reads, as in
  `SELECT id, name FROM accounts AS OF SYSTEM TIME '-10s' WHERE id = $1` or
  `... JOIN owners o ON o.id = a.owner_id AS OF SYSTEM TIME follower_read_timestamp()`:
  the clause changes no type, and the query stays read-only. As CockroachDB requires, it
  stands on the top-level query and takes a constant time or a call of
  `follower_read_timestamp`, `with_min_timestamp` or `with_max_staleness`; one in a
  subquery or with a parameter is [Q010](../diagnostics.md#q010).
- `ROLLUP`, `CUBE` and `GROUPING SETS`, which CockroachDB does not implement, are analysed
  as on PostgreSQL rather than reported.
- A column read outside every aggregate of a query that aggregates without `GROUP BY` is
  [Q062](../diagnostics.md#q062), an error, since CockroachDB rejects the query.

## Parameters

PostgreSQL style `$1`, `$2`, .... CockroachDB types a parameter from what its place wants,
so an argument of `concat` is text and an `OFFSET` value an integer, as PostgreSQL's are
not; only a parameter met first as the operand of `IS NULL`, which no place types, is
warned of as one the server cannot type ([Q063](../diagnostics.md#q063)). `@name` is a syntax error and `?` the
jsonb key test, as on PostgreSQL, so each is warned of as no parameter.

## Migrations

`migrate_backend_cockroachdb.New()`: the PostgreSQL backend with a lock table, because CockroachDB has
no advisory locks. `sgl migrate` selects it for `dialect: cockroachdb`; `lock: advisory` is
rejected.

- The lock is durable. Its transaction turns on `enable_durable_locking_for_serializable`
  before it locks the row, so a range split or move, which a backfilling schema change
  such as `ADD COLUMN ... NOT NULL DEFAULT` causes, cannot drop the lock and let a second
  replica in. This needs CockroachDB v23.2 or later; an older server fails the lock with
  `migrate.ErrUnsupportedLock`.
- The lock transaction refers to the lock table by its descriptor ID, so it holds no lease
  on the database or schema, and a migration can run `CREATE FUNCTION`, `CREATE SCHEMA`,
  `GRANT ... ON SCHEMA` or `ALTER DEFAULT PRIVILEGES` while the lock is held.
- Migrations do not run in transactions, because CockroachDB does not make a schema change
  atomic with the rest of a transaction. Each statement commits on its own, and a
  migration that fails part-way stays dirty; once the cause is fixed, the next `Up`
  resumes at the statement that failed.

See [Migrations and seeds](../migration-runtime.md#cockroachdb) for the rest.

## Drivers

`database/sql` (with `github.com/jackc/pgx/v5/stdlib`) or `pgx`.

## Exported declarations

### dialect_engine_cockroachdb

```go
func New(options ...Option) dialect.Dialect
type Option = postgres.Option
    func WithMaxParseDepth(depth int) Option
    func WithMaxTokensPerStatement(limit int) Option
    func WithName(name string) Option
    func WithSearchPath(schemas ...string) Option
```
