---
title: Use continuous aggregates
description: Create the continuous aggregate in a migration, then query its projected
  columns through Seagull. Keep refresh and retention administration aligned with
  the application’s data-freshness requirements.
section: How-to guides
subsection: Dialects
order: 630
---

# Use continuous aggregates

Create a continuous aggregate over a hypertable, refresh it, and read its typed summary columns. This example stores hourly statistics with TimescaleDB Toolkit.

## Create the hourly summary

Start with a [hypertable](timescaledb.md) and a server with TimescaleDB and Toolkit installed. Add this migration after the readings table:

```sql
-- sgl.migration(no_transaction: true)
CREATE MATERIALIZED VIEW hourly_readings
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 hour', ts) AS bucket,
    buoy,
    count(*) AS samples,
    stats_agg(temperature) AS stats
FROM readings
GROUP BY bucket, buoy;
```

The view uses `materialized_only = true`, so it contains only refreshed data. Add the refresh and read queries:

```sql
-- sgl.query(RefreshHourly, exec)
CALL refresh_continuous_aggregate('hourly_readings', NULL, NULL);
```

```sql
-- sgl.query(HourlyStats, many)
SELECT bucket, buoy, samples, average(stats) AS mean
FROM hourly_readings
ORDER BY bucket, buoy;
```

## Refresh outside a transaction

Run `sgl generate`, apply the migrations, and construct `store.New(database)`. After writing readings, call the refresh on the connection rather than through `RunInTx`:

```go
	if err := queries.RefreshHourly(ctx); err != nil {
		return fmt.Errorf("refreshing hourly_readings: %w", err)
	}
	fmt.Fprintln(output, "refreshing outside a transaction: done")
```

Then read the hourly rows:

```go
	hours, err := queries.HourlyStats(ctx)
	if err != nil {
		return fmt.Errorf("reading the hourly summaries: %w", err)
	}
	fmt.Fprintf(output, "after the refresh, hourly_readings has %d rows:\n", len(hours))
```

Choose refresh ranges or a refresh policy that meet your application’s freshness requirements; generation infers the view’s columns but never refreshes its data.

## Check the refresh and summaries

From the Seagull repository root, run the continuous-aggregate example:

```sh
cd examples/timescaledb/033_timescale_continuous_aggregates
go test .
```

Set `SGL_EXAMPLE_DSN` to a disposable database before running this server-backed test; without it, the test skips. See [running examples](run-examples.md) for connection setup.

The complete example produces:

```text
before the refresh, hourly_readings has 0 rows
refreshing in RunInTx: ERROR: refresh_continuous_aggregate() cannot run inside a transaction block (SQLSTATE 25001)
refreshing outside a transaction: done
after the refresh, hourly_readings has 6 rows:
  22:00  Doom Bar       readings 3  mean 14.20
  22:00  Stepper Point  readings 1  mean 15.00
  23:00  Doom Bar       readings 2  mean 13.70
  23:00  Stepper Point  readings 1  mean 14.60
  01:00  Doom Bar       readings 2  mean 13.20
  01:00  Stepper Point  readings 1  mean 14.00
the whole night, rolled up from the hourly summaries:
  Doom Bar       readings 7  mean 13.77  stddev 0.47  mean of hourly means 13.70
  Stepper Point  readings 3  mean 14.53  stddev 0.50  mean of hourly means 14.53
```

## Keep refreshes outside transactions

The example applies migrations with `migrate_backend_postgres.New()`, so its view migration declares `no_transaction: true`. `sgl migrate` and `migrate_backend_timescaledb.New()` recognise this statement and run it outside a transaction without the directive. The server also refuses `refresh_continuous_aggregate` inside a transaction, as the example output demonstrates.

See the [dialects/timescaledb reference](../reference/dialects/timescaledb.md) for the complete contract. The [example browser](https://seagull.sh/examples/) exposes other engine variants; use their actual SQL rather than changing only the dialect name.
