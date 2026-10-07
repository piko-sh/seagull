---
title: Use TimescaleDB hypertables
description: Choose the timescaledb dialect, declare the hypertable in migrations
  and use time_bucket in queries. Apply the migrations to a server with TimescaleDB
  installed.
section: How-to guides
subsection: Dialects
order: 620
---

# Use TimescaleDB hypertables

Create a TimescaleDB hypertable and query hourly summaries through typed Go methods. Use the `timescaledb` dialect and a PostgreSQL-family emitter.

The steps below use `sgl` and `sgl.yaml`. A program that generates or migrates from Go imports the TimescaleDB providers of the `sdk` kit instead:

| Role | Package | Constructor |
|---|---|---|
| Dialect | `seagull.sh/seagull/sdk/dialect/dialect_engine_timescaledb` | `dialect_engine_timescaledb.New()` |
| Emitter | `seagull.sh/seagull/sdk/emit/emit_driver_databasesql` or `seagull.sh/seagull/sdk/emit/emit_driver_pgx` | `emit_driver_databasesql.New()`, the default, or `emit_driver_pgx.New()` |
| Migrations | `seagull.sh/seagull/sdk/migrate/migrate_backend_timescaledb` | `migrate_backend_timescaledb.New()`, for `migrate.NewMigrator` |
| Live schema | `seagull.sh/seagull/sdk/schema/schema_source_postgres` | `schema_source_postgres.New(database, engine)`, with the TimescaleDB dialect as `engine` |

## Create the hypertable and bucket query

Choose `dialect: timescaledb` in your database configuration and use a server with TimescaleDB installed. Add the extension, source table and hypertable call to a migration:

```sql
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE readings (
    ts          TIMESTAMPTZ      NOT NULL,
    buoy        TEXT             NOT NULL,
    temperature DOUBLE PRECISION NOT NULL
);

SELECT create_hypertable('readings', 'ts', chunk_time_interval => INTERVAL '1 day');
```

Put the hourly query in a query file:

```sql
-- sgl.query(HourlyAverages, many)
SELECT time_bucket(INTERVAL '1 hour', ts) AS bucket, avg(temperature) AS mean
FROM readings
WHERE buoy = $1
GROUP BY bucket
ORDER BY bucket;
```

The bucket interval is SQL, and the buoy is a bound input. The [complete query file](../../examples/timescaledb/032_timescale_hypertables/db/queries/readings.sql) also covers writes, gap filling and first/last readings.

## Read the typed hourly summaries

Run `sgl generate`, apply the migrations, write representative readings and construct `store.New(database)`. Read one buoy’s summaries:

```go
	hours, err := queries.HourlyAverages(ctx, "Doom Bar")
	if err != nil {
		return fmt.Errorf("averaging by the hour: %w", err)
	}
	fmt.Fprintln(output, "Doom Bar by the hour (time_bucket):")
	for _, hour := range hours {
		fmt.Fprintf(output, "  %s  mean %s\n", hour.Bucket.UTC().Format(clock), celsius(hour.Mean))
	}
	return nil
```

When a missing hour must appear, use `time_bucket_gapfill` with explicit start and finish bounds, as the example’s `GapfilledHourly` does. For persisted summaries, [create a continuous aggregate](continuous-aggregates.md).

## Check bucketing and missing hours

From the Seagull repository root, run the hypertable example:

```sh
cd examples/timescaledb/032_timescale_hypertables
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
recorded 10 readings in 2 chunks
Doom Bar by the hour (time_bucket):
  22:00  mean 14.2
  23:00  mean 13.7
  01:00  mean 13.2
Doom Bar by the hour, gaps filled (time_bucket_gapfill, locf):
  22:00  mean 14.2  carried 14.2
  23:00  mean 13.7  carried 13.7
  00:00  mean    -  carried 13.7
  01:00  mean 13.2  carried 13.2
first and last reading (first, last):
  Doom Bar      first 14.0  last 13.3
  Stepper Point first 15.0  last 14.0
```

## Keep server setup separate from inference

Seagull types the projected buckets and hyperfunctions; the server supplies the extension and executes them. Apply and query against the TimescaleDB version you deploy, especially when using toolkit or newer hypertable declarations.

See the [dialects/timescaledb reference](../reference/dialects/timescaledb.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
