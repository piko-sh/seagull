// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"seagull.sh/seagull/examples/postgres/023_transactions/internal/store"
)

// sqlStateError is a server error that carries a SQLSTATE code, as pgx's *pgconn.PgError
// does.
type sqlStateError interface {
	error

	// SQLState returns the five-character SQLSTATE code.
	SQLState() string
}

// run opens two accounts in a transaction it manages itself, then makes two transfers
// with RunInTx: one commits, and one breaks the balance check part-way and rolls back.
//
// sgl.yaml sets go.interface, so the generated package also declares store.Querier, an
// interface of every query method, which *store.Queries implements whether it runs on the
// database or inside a transaction. printBalances takes a store.Querier, so the same
// function reads the committed balances and, inside each transfer, the balances the
// transaction sees before it commits or rolls back; a test could pass it a fake instead.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each step and the balances.
//
// Returns error when a query fails other than as the example expects.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	pip, gull, err := openAccounts(ctx, database, queries)
	if err != nil {
		return fmt.Errorf("opening accounts: %w", err)
	}
	fmt.Fprintln(output, "opened accounts for Pip and Gull: committed")
	if err := printBalances(ctx, queries, output, "balances"); err != nil {
		return err
	}

	if err := transfer(ctx, database, queries, output, pip, gull, 1500); err != nil {
		return fmt.Errorf("first transfer: %w", err)
	}
	fmt.Fprintln(output, "transferred 1500 from Pip to Gull: committed")
	if err := printBalances(ctx, queries, output, "balances"); err != nil {
		return err
	}

	// Gull holds 3500, so this transfer credits Pip and then fails the CHECK constraint when
	// it debits Gull. RunInTx rolls back, and the credit to Pip that had already run is
	// undone with it.
	err = transfer(ctx, database, queries, output, gull, pip, 9000)
	if err == nil {
		return errors.New("an overdrawing transfer committed")
	}

	// pgx reports a server error as a *pgconn.PgError, whose SQLState method gives the
	// standard five-character code: 23514 for a failed CHECK constraint. Match on the code,
	// not the message, as the wording differs between servers and versions.
	failure, hasState := errors.AsType[sqlStateError](err)
	if !hasState {
		return fmt.Errorf("an overdrawing transfer failed without a SQLSTATE: %w", err)
	}
	fmt.Fprintf(output, "transferred 9000 from Gull to Pip: rolled back (SQLSTATE %s)\n", failure.SQLState())
	return printBalances(ctx, queries, output, "balances")
}

// openAccounts opens both accounts in one transaction that it begins and commits itself.
// WithTx returns a copy of queries bound to that transaction, which suits a transaction
// begun with options or shared with code that does not use Seagull.
//
// Takes database (*sql.DB) which begins the transaction.
// Takes queries (*store.Queries) which are bound to it.
//
// Returns pipID (int64) which is Pip's account ID.
// Returns gullID (int64) which is Gull's account ID.
// Returns err (error) when an account cannot be opened or the commit fails.
func openAccounts(ctx context.Context, database *sql.DB, queries *store.Queries) (pipID, gullID int64, err error) {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	// Rollback after a successful Commit does nothing, so deferring it covers every early
	// return.
	defer func() { _ = transaction.Rollback() }()

	inTransaction := queries.WithTx(transaction)
	pip, err := inTransaction.OpenAccount(ctx, store.OpenAccountParams{Owner: "Pip", Balance: 5000})
	if err != nil {
		return 0, 0, err
	}
	gull, err := inTransaction.OpenAccount(ctx, store.OpenAccountParams{Owner: "Gull", Balance: 2000})
	if err != nil {
		return 0, 0, err
	}
	return pip.ID, gull.ID, transaction.Commit()
}

// transfer moves an amount between two accounts with RunInTx, which begins a transaction,
// passes fn a Queries bound to it, and commits when fn returns nil or rolls back when it
// returns an error. After the credit it prints the balances the transaction sees, through
// the same printBalances the caller uses.
//
// Takes database (*sql.DB) which begins the transaction.
// Takes queries (*store.Queries) which run the transfer.
// Takes output (io.Writer) which receives the balances inside the transaction.
// Takes from (int64) which is the account to debit.
// Takes to (int64) which is the account to credit.
// Takes amount (int32) which is the sum to move, in pence.
//
// Returns error when either update fails; the transaction is then rolled back.
func transfer(ctx context.Context, database *sql.DB, queries *store.Queries, output io.Writer, from, to int64, amount int32) error {
	return queries.RunInTx(ctx, database, func(inTransaction *store.Queries) error {
		if err := inTransaction.Deposit(ctx, store.DepositParams{Amount: amount, ID: to}); err != nil {
			return err
		}
		if err := printBalances(ctx, inTransaction, output, "inside the transaction"); err != nil {
			return err
		}
		return inTransaction.Withdraw(ctx, store.WithdrawParams{Amount: amount, ID: from})
	})
}

// printBalances writes every account's balance on one line. It takes the generated
// store.Querier rather than *store.Queries, so it reads through whatever it is given: the
// queries on the database, the queries bound to a transaction, or a fake in a test.
//
// Takes queries (store.Querier) which read the accounts.
// Takes output (io.Writer) which receives the line.
// Takes label (string) which says whose view of the balances the line shows.
//
// Returns error when the accounts cannot be listed.
func printBalances(ctx context.Context, queries store.Querier, output io.Writer, label string) error {
	accounts, err := queries.ListAccounts(ctx)
	if err != nil {
		return fmt.Errorf("listing accounts: %w", err)
	}
	fmt.Fprintf(output, "  %s:", label)
	for _, account := range accounts {
		fmt.Fprintf(output, " %s %d", account.Owner, account.Balance)
	}
	fmt.Fprintln(output)
	return nil
}
