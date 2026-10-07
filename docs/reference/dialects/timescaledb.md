---
title: TimescaleDB
description: "TimescaleDB generation, supported extensions, type mapping and migration behaviour."
section: Reference
subsection: Dialects
order: 140
---

# TimescaleDB

TimescaleDB extends PostgreSQL with hypertables, continuous aggregates and time-series
functions. Seagull reuses PostgreSQL's parser and type rules, adding TimescaleDB and
toolkit function signatures, opaque aggregate-state types and statement metadata.

This reference describes the recognised forms and their catalogue metadata. Follow
[the TimescaleDB guide](../../how-to/timescaledb.md) to create and query a hypertable.

## Capability summary

| Concern | Contract |
|---|---|
| Parser family | PostgreSQL |
| Generated API | database/sql or pgx |
| Live introspection | Yes, PostgreSQL-family source |
| Runtime migrations | `migrate_backend_timescaledb.New()`, which the CLI selects for this dialect: PostgreSQL's locking and history, and the statements TimescaleDB cannot run in a transaction run outside one without a directive. |

[Known limitations](../limitations.md#timescaledb) records inference gaps and workarounds; the
[driver matrix](../driver-matrix.md) compares generated APIs.

The [engine guide](../../how-to/timescaledb.md) and [runnable examples](../../../examples/timescaledb) cover execution. The declarations below link to constructor contracts. Parser limits affect analysis, not server execution.

## Setup

Select the `timescaledb` dialect for the database in `sgl.yaml`. It uses PostgreSQL-family transactions and advisory locks, and `sgl migrate` runs it on the TimescaleDB backend:

```yaml
databases:
  - name: metrics
    dialect: timescaledb
    migrations: db/migrations
    queries: db/queries
    go:
      package: metrics
      out: internal/metrics
```

A program that runs its own migrations uses the same backend, not `migrate_backend_postgres.New()`:

```go
migrator, err := migrate.NewMigrator(database, migrate_backend_timescaledb.New(), migrations, "db/migrations")
```

Two TimescaleDB statements fail inside a transaction: a continuous aggregate created with
its data (`CREATE MATERIALIZED VIEW ... WITH (timescaledb.continuous)` without
`WITH NO DATA`) and `CALL refresh_continuous_aggregate(...)`. The TimescaleDB backend
recognises both and runs a migration holding one without a transaction, tracking its
progress statement by statement as it does for any non-transactional migration, so no
directive is needed. A migration that holds one beside other statements runs all of them
without a transaction, which [Q043](../diagnostics.md#q043) warns of; give such a
statement a migration of its own. `-- sgl.migration(no_transaction: true)` still turns the
transaction off for any other statement that needs it; see
[Migrations and seeds](../migration-runtime.md#transactions).

## DDL forms Seagull recognises

### CREATE HYPERTABLE (keyword form)

```sql
CREATE HYPERTABLE IF NOT EXISTS readings (
    ts          TIMESTAMPTZ NOT NULL,
    device_id   BIGINT,
    temperature DOUBLE PRECISION
) PARTITION BY RANGE (ts);
```

Creates the table as `CREATE TABLE` does, and records `TIMESCALE_HYPERTABLE = "true"` in the TimescaleDB metadata the dialect keeps for the statement. Schema-qualified names (`analytics.readings`) work the same way. If `IF NOT EXISTS` is present, Seagull also sets `TIMESCALE_IF_NOT_EXISTS = "true"`. Seagull captures trailing `PARTITION BY` / `WITH` clauses opaquely into `TIMESCALE_TRAILING`.

The column list is read exactly as `CREATE TABLE` reads one: a `serial` or
`GENERATED ... AS IDENTITY` key is `NOT NULL` with a default, so an `INSERT` may leave it
out, and table constraints and `LIKE` copies are kept.

### create_hypertable() function call

```sql
SELECT create_hypertable('readings', 'ts', chunk_time_interval => INTERVAL '1 day');
```

Annotates the table a `CREATE TABLE` made earlier, changing none of its columns. `TIMESCALE_HYPERTABLE = "true"`,
`TIMESCALE_TIME_COLUMN = "ts"`, and `TIMESCALE_ANNOTATE_ONLY = "true"` mark it as a
hypertable promotion of an already-created table. A dimension builder as the second
argument, as in `create_hypertable('readings', by_range('ts'))`, also sets
`TIMESCALE_DIMENSION_BUILDER` (`by_range` or `by_hash`). Seagull preserves extra named
arguments (`chunk_time_interval => ...`, `space_partitions => ...`) in
`TIMESCALE_CALL_EXTRAS`, as written and joined by `, `.

### CREATE TABLE ... WITH (tsdb.hypertable)

```sql
CREATE TABLE readings (ts TIMESTAMPTZ NOT NULL, device_id BIGINT)
WITH (tsdb.hypertable, tsdb.partition_column = 'ts', tsdb.chunk_interval = '1 day');
```

The form TimescaleDB 2.18 added is an ordinary `CREATE TABLE`, which creates the table
as usual; a `tsdb.hypertable` or `timescaledb.hypertable` key in its `WITH`
list adds `TIMESCALE_HYPERTABLE = "true"`, and the `partition_column`, `chunk_interval`,
`segmentby`, `orderby` and `enable_columnstore` keys of either namespace become
`TIMESCALE_TIME_COLUMN`, `TIMESCALE_CHUNK_TIME_INTERVAL`, `TIMESCALE_SEGMENTBY`,
`TIMESCALE_ORDERBY` and `TIMESCALE_COLUMNSTORE_ENABLED`, without their quotes; any other
key of either namespace keeps its value, a string without its quotes, under
`TIMESCALE_RELOPTION_<KEY>`. A table whose `WITH` list holds no hypertable key, such as
`WITH (fillfactor = 80)`, gets no TimescaleDB metadata.

### CREATE MATERIALIZED VIEW with timescaledb.continuous

```sql
CREATE MATERIALIZED VIEW hourly_temps
WITH (timescaledb.continuous = true, timescaledb.materialized_only = true)
AS SELECT time_bucket('1 hour', ts) AS bucket, avg(temperature)
FROM readings
GROUP BY bucket
WITH NO DATA;
```

Creates the view, recording:

- `TIMESCALE_CONTINUOUS_AGGREGATE = "true"`
- `TIMESCALE_CONTINUOUS_FLAG = "true"` (from the reloption itself)
- `TIMESCALE_MATERIALIZED_ONLY = "true"`
- `TIMESCALE_WITH_DATA = "false"` (`"true"` without `WITH NO DATA`)
- `TIMESCALE_VIEW_BODY = <the SELECT as written>`

`IF NOT EXISTS` sets `TIMESCALE_IF_NOT_EXISTS = "true"`. The other `timescaledb.*` keys
TimescaleDB takes there become keys of their own, `TIMESCALE_CREATE_GROUP_INDEXES`,
`TIMESCALE_FINALIZED`, `TIMESCALE_COMPRESSION_*`, `TIMESCALE_COLUMNSTORE_ENABLED`,
`TIMESCALE_SEGMENTBY` and `TIMESCALE_ORDERBY`, and an unknown key keeps its value under
`TIMESCALE_RELOPTION_<KEY>`.

The aggregate's columns are typed from its `SELECT`, as any view's are, so a query reading
the aggregate gets typed fields: `time_bucket` gives a `timestamptz`, and an aggregate such
as `avg` a nullable value. Regular `CREATE MATERIALIZED VIEW name AS SELECT ...` (without
the `timescaledb.continuous` reloption) takes the built-in PostgreSQL path with no
TimescaleDB metadata.

### ALTER TABLE / ALTER MATERIALIZED VIEW SET (timescaledb.*)

```sql
ALTER TABLE readings SET (
    timescaledb.compress = true,
    timescaledb.compress_segmentby = 'device_id',
    timescaledb.compress_orderby = 'ts DESC'
);
```

Annotates the table or view, changing none of its columns, and records:

- `TIMESCALE_COMPRESSION_ENABLED = "true"`
- `TIMESCALE_COMPRESSION_SEGMENTBY = "device_id"`
- `TIMESCALE_COMPRESSION_ORDERBY = "ts DESC"`

The `ALTER MATERIALIZED VIEW` form additionally sets `TIMESCALE_ALTER_MATERIALIZED = "true"`. Seagull preserves unrecognised `timescaledb.*` keys in `TIMESCALE_RELOPTION_<KEY>` so it drops nothing silently.

`ALTER TABLE x SET SCHEMA y` takes the built-in PostgreSQL path (it is not a compression operation).

## Management calls Seagull recognises

### Policy, job and hypertable management

```sql
SELECT add_retention_policy('readings', INTERVAL '30 days');
SELECT add_dimension('readings', by_hash('device_id', 4));
```

A migration's `SELECT` of one of these, the function called on its own, annotates the
table, changing no column, with `TIMESCALE_POLICY_OP` naming
the function: `add_compression_policy`, `add_columnstore_policy`, `add_retention_policy`,
`add_continuous_aggregate_policy` and `add_reorder_policy` with their `remove_` forms,
`add_job`, `alter_job`, `delete_job`, `run_job`, `add_dimension`,
`set_chunk_time_interval`, `set_integer_now_func`, `enable_chunk_skipping` and
`disable_chunk_skipping`. The first argument becomes `TIMESCALE_TABLE`, or
`TIMESCALE_JOB_ID` for the job calls that take one and `TIMESCALE_JOB_PROC` for `add_job`.
`add_dimension`'s column becomes `TIMESCALE_TIME_COLUMN`, with `TIMESCALE_DIMENSION_BUILDER`
for a builder, and the remaining arguments are kept as written, joined by `, `, in
`TIMESCALE_POLICY_ARGS`. In a query file the same call is an ordinary query, which the
catalogue types and routes to the writer.

In a migration, the call must end the statement, as must `create_hypertable(...)` and
`CALL refresh_continuous_aggregate(...)`: a call followed by `FROM`, `WHERE` or other text
is a syntax error ([Q010](../diagnostics.md#q010)). Move such a call into a statement
of its own. A quoted name, such as `"create_hypertable"` or `"timescaledb".compress`, is
never read as a TimescaleDB keyword and takes the PostgreSQL path.

### CALL refresh_continuous_aggregate

`CALL refresh_continuous_aggregate('hourly_temps', NULL, NULL)` annotates the aggregate
with
`TIMESCALE_POLICY_OP = "refresh_continuous_aggregate"`,
`TIMESCALE_REFRESH_CONTINUOUS_AGGREGATE_TARGET` and the window bounds as text in
`TIMESCALE_REFRESH_WINDOW_START` and `TIMESCALE_REFRESH_WINDOW_END`.

## Function library

The catalogue includes bucketing, gap filling, first/last, statistics, counters,
candlesticks, approximate aggregates and their accessors, plus policy, job and hypertable
management.
The literal forms TimescaleDB's documentation uses type without casts, as on PostgreSQL:
`time_bucket('1 hour', time)` reads the quoted width as an interval, so the bucket of a
query or of a continuous aggregate is a `timestamptz`, and `approx_percentile(0.95, ...)`
or `uddsketch(100, 0.01, value)` read the numeric literals as `double precision`.

A set-returning function in a FROM clause has the columns TimescaleDB and the toolkit
declare for it: `topn(state, 5) AS value` has one column named after its alias, as any
function returning a set of a base type does on PostgreSQL, and `live_ranges`,
`dead_ranges`, `state_timeline`, `state_int_timeline`, `state_periods`, their
`interpolated_` forms, `hypertable_detailed_size`, `hypertable_approximate_detailed_size`,
`chunks_detailed_size` and the compression and columnstore statistics have the columns of
their `RETURNS TABLE` clauses. A name whose overloads return different columns takes the
columns of the overload its argument selects: `into_values` of a state aggregate reads
`state` and `duration`, of a frequency aggregate `value`, `min_freq` and `max_freq`, and
the toolkit's `unnest` of a `timevector`, which shares its name with PostgreSQL's
`unnest`, reads `time` and `value`. An argument of no known type selects nothing, and
the name alone decides, as for PostgreSQL's `unnest`.

The toolkit's accessor arrows apply the accessor's function to the aggregate state on
their left, as the toolkit's `->` operators do: `stats_agg(reading) -> average()` reads as
`average(stats_agg(reading))`, a `double precision`, `percentile_agg(reading) ->
approx_percentile(0.95)` as `approx_percentile(0.95, percentile_agg(reading))`, and
`stats_agg(reading) -> num_vals()` as a `bigint`. The accessors (`average()`,
`approx_percentile(0.95)`, `stddev('population')` and the rest) are known functions of
their own accessor types, so a pipeline raises no unknown-function warning. An arrow whose
state the catalogue does not hold a function of the accessor for, such as `min_n(...) ->
into_array()`, whose states Seagull merges into one `min_n_state`, or `approx_percentiles`,
which the toolkit gives no function form, reads as `any`; on a JSON document `->` keeps
its meaning.

Seagull registers the aggregate-state aliases (`statssummary1d`, `counter_summary`, `candlestick`, `tdigest`, and the rest) as opaque (unknown-category) types that codegen emits as `any`. Override the Go destination per column by naming the output column in a query header:

```sql
-- sgl.query(GetCounterState, one)
-- sgl.column(state, go_type: "github.com/your/pkg.YourType")
SELECT counter_agg(ts, value) AS state FROM readings WHERE device_id = $1;
```

## Limitations

- As on PostgreSQL, a column read outside every aggregate of a query that aggregates
  without `GROUP BY` is [Q062](../diagnostics.md#q062), an error, since the engine
  rejects the query.
- The other precision gaps TimescaleDB shares with PostgreSQL, and its own, are listed on
  the [Known limitations](../limitations.md#timescaledb) page.

## Configuring the dialect

`dialect_engine_timescaledb.New` builds on the PostgreSQL dialect with TimescaleDB's
statements, functions and lexical rules already registered. Its options are the ones the
[exported declarations](#exported-declarations) list: `WithName`, `WithSearchPath` and the
two parser limits, which [configure a built-in dialect](../../how-to/configure-a-dialect.md)
shows in use. Third-party dialects, a child of the PostgreSQL dialect among them, are not
supported yet.

## Exported declarations

### dialect_engine_timescaledb

```go
func New(options ...Option) dialect.Dialect
type Option = postgres.Option
    func WithMaxParseDepth(depth int) Option
    func WithMaxTokensPerStatement(limit int) Option
    func WithName(name string) Option
    func WithSearchPath(schemas ...string) Option
```
