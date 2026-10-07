---
title: Batch writes and use COPY
description: Choose batch for a collection of input rows. For pgx, use copyfrom when
  COPY semantics fit the table and caller. Pass a slice of generated Params values
  and check the operation’s returned error.
section: How-to guides
subsection: Writes
order: 520
---

# Batch writes and use COPY

Use `batch` to send a collection of inputs through one generated call. With pgx, choose `copyfrom` when the table and input fit PostgreSQL COPY semantics. Both take a slice of generated Params values.

## Declare the batch query

For database/sql and native ClickHouse, use an INSERT with one VALUES row containing every parameter. For example:

```sql
-- sgl.query(RecordReadings, batch)
INSERT INTO readings (station, minute, height_cm, note) VALUES (?, ?, ?, ?);
```

Run `sgl vet` and `sgl generate`. database/sql repeats the VALUES row in statements sized to the bind limit; an upsert or another clause after VALUES can require one statement per item. A row that alone binds more variables than one statement allows is refused at generation with [Q060](../reference/diagnostics.md#q060); insert fewer columns per statement, or move values every row shares into the SQL. An empty input slice sends nothing.

pgx batches instead queue one statement per item, so an UPDATE or DELETE can be a batch too. Result rows are discarded; choose a row-returning command when the caller needs them.

## Pass one Params value per input

Create `Queries` from the connection and build a slice with one Params value for each reading:

```go
readings := []store.RecordReadingsParams{
    {Station: "harbour", Minute: 0, HeightCm: 200},
    {Station: "pier", Minute: 0, HeightCm: 180},
}
```

These readings have no note, so `Note` keeps its nil value. Pass the slice to the generated method and check the write error:

```go
	if err := queries.RecordReadings(ctx, readings); err != nil {
		return fmt.Errorf("recording readings: %w", err)
	}
```

A successful call means all its statements completed. A database/sql call can contain several statements, so a later failure can leave earlier chunks committed. Retrying the complete input can then repeat those writes. With the preset's `WrapErrors` set, the error names the row its failing statement starts at, counted from zero (`sgl: RecordReadings: the statement starting at row 499: ...`), so the rows before it are the ones already written. A pgx batch returns the first failing statement's error, or the error pgx reports when it closes the batch, such as a deferred constraint failing at commit.

## Make the complete write atomic when required

Use [RunInTx or WithTx](transactions.md) and call the batch on the transaction-bound `Queries` when every input must commit or roll back together. For example, with database/sql:

```go
err := queries.RunInTx(ctx, database, func(inTransaction *store.Queries) error {
    return inTransaction.RecordReadings(ctx, readings)
})
```

Check `err` before considering the import complete. Native ClickHouse has no generated transaction helper, so choose an application retry policy suited to its write semantics.

## Choose COPY for compatible pgx inputs

Change the command to `copyfrom` for an INSERT naming its target columns and holding only placeholders in its VALUES row. COPY does not apply clauses after VALUES, including upserts or RETURNING, and some PostgreSQL types require connection codec registration that generated code cannot assume. Inspect the diagnostics rather than replacing an upsert with COPY blindly.

pgx `copyfrom` returns a row count and an error. Other emitters return only an error. The [query commands reference](../reference/query-commands.md) gives the complete restrictions for each emitter.

The [working example](../../examples/sqlite/022_bulk_writes) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/022_bulk_writes
go test .
```
