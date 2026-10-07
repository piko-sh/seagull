---
title: Test generated queries
description: Test the generated package through the public methods your application
  uses. A successful generation proves neither database connectivity nor the business
  meaning of a query.
section: How-to guides
subsection: Tooling
order: 970
---

# Test generated queries

Test the public methods your application calls against a database with the expected schema. Assert the returned values and the effect on stored data.

## Start with a small fixture

Open a temporary SQLite file, or a disposable database for your production dialect. Apply the migrations, create the generated `Queries` value and insert only the rows the test needs.

This test from the [first tutorial](../../tests/tutorials/01_first_query/main_test.go) uses the shared connection setup, calls the application and checks its output:

```go
func TestCheckpoint(t *testing.T) {
	db, err := support.Open(t.Context(), migrations, filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var output bytes.Buffer
	if err := run(t.Context(), db, &output); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("output.txt")
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want) {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}
```

For a query test, assert its returned fields directly. Give collection queries an explicit `ORDER BY` when the test depends on order.

## Test the failure that matters

Add cases for behaviour the caller relies on, such as a missing row, a NULL field or a constraint failure. A transaction test should check that the write was rolled back as well as checking the error. [Tutorial 5](../tutorials/05-testing.md) builds that test and deliberately makes it fail.

Run integration tests again when changing the emitter or driver, since binding and scanning depend on that choice. Change SQL, configuration or handwritten calling code to fix a failure, then regenerate.

Use [the driver examples](run-examples.md) for database-specific setup and [CI checks](ci.md) to keep generation and runtime tests in the build.
