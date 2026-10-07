---
title: Run queries in a transaction
description: "Use RunInTx for a callback whose nil result commits and whose error rolls back."
section: How-to guides
subsection: Connections
order: 530
---

# Run queries in a transaction

Use `RunInTx` when several generated queries must commit together. If the application already owns a transaction, use `WithTx` to direct the queries to it. Both helpers are available with database/sql and pgx; native ClickHouse has neither.

## Commit a group of queries together

Create `Queries` from your connection, then pass `RunInTx` a callback that uses the transaction-bound value it receives. Return an error as soon as an operation fails. In the [SQLite transfer example](../../examples/sqlite/023_transactions), crediting the recipient and debiting the sender belong to the same transaction:

```go
func transfer(ctx context.Context, database *sql.DB, queries *store.Queries, from, to, amount int64) error {
	return queries.RunInTx(ctx, database, func(inTransaction *store.Queries) error {
		if err := inTransaction.Deposit(ctx, store.DepositParams{Amount: amount, ID: to}); err != nil {
			return err
		}
		return inTransaction.Withdraw(ctx, store.WithdrawParams{Amount: amount, ID: from})
	})
}
```

`RunInTx` begins the transaction and commits when the callback returns nil. If the debit fails, it rolls back the credit too. Check the returned error, which can also report a failure to begin or commit the transaction.

Every operation in the callback must use `inTransaction`. A call on the original `queries` still uses the original connection and falls outside this transaction.

## Use a transaction the application already owns

When another part of the application begins a transaction, bind the generated queries to it:

```go
	inTransaction := queries.WithTx(transaction)
```

Call the generated methods on that value and leave commit and rollback with the transaction's owner. The example's [openAccounts function](../../examples/sqlite/023_transactions/example.go) shows `BeginTx`, deferred rollback and explicit commit around two account inserts.

With database/sql, `RunInTx` takes the `*sql.DB` that begins the transaction. With pgx it takes a generated `DBTX`, such as a pool or connection. A prepared database/sql wrapper keeps its prepared statements in the transaction; pass `prepared.UnderlyingDB()` to `RunInTx`, as the [preparation guide](prepared-statements.md) shows.

## Check the rollback path

Exercise a failure after an earlier query has succeeded, then check that none of the transaction's changes remain. The [example test](../../examples/sqlite/023_transactions/main_test.go) transfers more than the sender owns and verifies unchanged balances after the error. To run it from the repository root:

```sh
cd examples/sqlite/023_transactions
go test .
```

See the [generated API reference](../reference/generated-go.md#queriergo) for driver signatures and wrapper behaviour.
