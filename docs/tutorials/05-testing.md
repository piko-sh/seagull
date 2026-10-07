---
title: Test your data layer
description: Prove that the task application rolls back a failed transaction. The
  test will assert both the returned error and the surviving data.
section: Tutorials
subsection: Build a task application
order: 50
---

# Test your data layer

Prove that a failed transaction leaves the task table unchanged. The test will check both the error and the surviving data.

## Starting point

Continue in the practice application after [migrations](04-migrations.md).

## Step 1: Define the failure

Add `"errors"` to the imports in `main.go`:

```go
"errors"
```

Append this code before the final `return nil` in `run`:

```go
	stop := errors.New("roll back this transaction")
	err = queries.RunInTx(ctx, db, func(tx *store.Queries) error {
		if _, err := tx.CreateTask(ctx, "Do not keep this task"); err != nil {
			return err
		}
		return stop
	})
	if !errors.Is(err, stop) {
		return fmt.Errorf("expected rollback error, got %v", err)
	}
	tasks, err := queries.ListTasks(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "tasks after rollback:", len(tasks))
```

The callback inserts a second task, then returns `stop`. `RunInTx` rolls the transaction back. The caller checks that error and counts tasks through the outer connection.

```sh
go run .
```

```text
1 Feed the seagulls
Bring chips
missing: true
Feed the seagulls in Harbour
priority: 1
tasks after rollback: 1
```

Only the original task remains.

## Step 2: Check it in a test

Create `main_test.go`:

```go
package main

import (
    "bytes"
    "path/filepath"
    "strings"
    "testing"

    "seagull.sh/seagull/tests/tutorials/internal/support"
)

func TestRollback(t *testing.T) {
    db, err := support.Open(t.Context(), migrations, filepath.Join(t.TempDir(), "tasks.db"))
    if err != nil {
        t.Fatal(err)
    }
    defer db.Close()
    var output bytes.Buffer
    if err := run(t.Context(), db, &output); err != nil {
        t.Fatal(err)
    }
    if !strings.HasSuffix(output.String(), "tasks after rollback: 1\n") {
        t.Fatalf("unexpected surviving tasks:\n%s", output.String())
    }
}
```

The temporary database keeps each test independent. If the callback returns the wrong error, `run` fails. If the inserted task survives, the final assertion fails.

```sh
go test . -count=1
```

## Step 3: See the test fail

Inside the callback, replace `return stop` with `return nil`:

```go
return nil // commit for this experiment
```

Run the test again:

```sh
go test . -count=1
```

Its failure includes:

```text
expected rollback error
```

A successful callback committed the transaction instead of requesting rollback. Restore the original return:

```go
return stop
```

```sh
go test . -count=1
```

Continue with [CI checks](06-ci.md). Use the same approach against your production database when testing behaviour that differs between engines.
