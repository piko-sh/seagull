---
title: Stream rows and stop early
description: Choose the stream command and range over the generated iterator. Check
  each yielded error and return or break when enough rows have been processed. Pass
  a cancellable context from the caller.
section: How-to guides
subsection: Results
order: 470
---

# Stream rows and stop early

Choose `stream` when callers should process rows as they arrive rather than buffer the complete result. Pass a cancellable context and check every yielded error.

## Generate a streaming method

Use the stream command on a row-returning query:

```sql
-- sgl.query(StreamReadings, stream)
SELECT taken_at, level_cm FROM readings ORDER BY taken_at;
```

Run `sgl vet` and `sgl generate`. The method returns a range-over-func iterator instead of a slice.

## Process each row and stop when ready

Range over the iterator, checking the error before using its row. This example stops as soon as the level reaches the flood-warning threshold:

```go
	read := 0
	for reading, err := range queries.StreamReadings(ctx) {
		if err != nil {
			return fmt.Errorf("streaming readings: %w", err)
		}
		read++
		if reading.LevelCm >= floodWarningCm {
			fmt.Fprintf(output, "flood warning at %s, %dcm, after reading %d of %d rows\n", reading.TakenAt, reading.LevelCm, read, count)
			break
		}
	}
```

Breaking or returning from the loop closes the rows. Let the context expire or cancel it when the surrounding request no longer needs the query.

A database or scan error can arrive after earlier rows have been consumed. If the loop yields an error, report a failed or partial operation rather than treating the rows seen so far as the complete result.

See the [query commands reference](../reference/query-commands.md) for the generated signature. The [example](../../examples/sqlite/017_streaming) also scans every reading to compute the high-water mark; its test checks both complete and early-stop paths.

The [working example](../../examples/sqlite/017_streaming) includes connection setup and tests. From the repository root, run:

```sh
cd examples/sqlite/017_streaming
go test .
```
