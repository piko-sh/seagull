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

	"seagull.sh/seagull/examples/cockroachdb/023_transactions/internal/store"
)

const (
	// serialisationFailure is the SQLSTATE CockroachDB reports when it aborts a transaction
	// that conflicted with another, so that the client can run it again.
	serialisationFailure = "40001"

	// maxAttempts bounds how many times retrySerialisable runs a transaction.
	maxAttempts = 3
)

// sqlStateError is the method a driver's error offers to report its SQLSTATE. pgx's
// *pgconn.PgError has it, so code can read the state without importing pgx.
type sqlStateError interface {
	error

	// SQLState returns the five-character SQLSTATE code.
	SQLState() string
}

// run opens two accounts in a transaction it manages itself, then makes two transfers
// with RunInTx: one commits, and one breaks the balance check part-way and rolls back.
// Last, it pays interest with a read-then-write transaction that another client's deposit
// interrupts, which CockroachDB aborts and retrySerialisable runs again.
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
	if err := printBalances(ctx, queries, output); err != nil {
		return err
	}

	if err := transfer(ctx, database, queries, pip, gull, 1500); err != nil {
		return fmt.Errorf("first transfer: %w", err)
	}
	fmt.Fprintln(output, "transferred 1500 from Pip to Gull: committed")
	if err := printBalances(ctx, queries, output); err != nil {
		return err
	}

	// Gull holds 3500, so this transfer credits Pip and then fails the CHECK constraint when
	// it debits Gull. RunInTx rolls back, and the credit to Pip that had already run is
	// undone with it.
	err = transfer(ctx, database, queries, gull, pip, 9000)
	if err == nil {
		return errors.New("an overdrawing transfer committed")
	}
	fmt.Fprintf(output, "transferred 9000 from Gull to Pip: rolled back (%v)\n", err)
	if err := printBalances(ctx, queries, output); err != nil {
		return err
	}

	aborted, err := payInterest(ctx, database, queries, gull)
	if err != nil {
		return fmt.Errorf("paying interest: %w", err)
	}
	fmt.Fprintln(output, "paid Gull 10% interest while a deposit of 100 landed:")
	// A 40001 message names timestamps and transaction IDs that change on every run, so only
	// its SQLSTATE is printed.
	for index, abort := range aborted {
		fmt.Fprintf(output, "  attempt %d: aborted with SQLSTATE %s, retried\n", index+1, sqlState(abort))
	}
	fmt.Fprintf(output, "  attempt %d: committed\n", len(aborted)+1)
	return printBalances(ctx, queries, output)
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
// returns an error.
//
// Takes database (*sql.DB) which begins the transaction.
// Takes queries (*store.Queries) which run the transfer.
// Takes from (int64) which is the account to debit.
// Takes to (int64) which is the account to credit.
// Takes amount (int64) which is the sum to move, in pence.
//
// Returns error when either update fails; the transaction is then rolled back.
func transfer(ctx context.Context, database *sql.DB, queries *store.Queries, from, to, amount int64) error {
	return queries.RunInTx(ctx, database, func(inTransaction *store.Queries) error {
		if err := inTransaction.Deposit(ctx, store.DepositParams{Amount: amount, ID: to}); err != nil {
			return err
		}
		return inTransaction.Withdraw(ctx, store.WithdrawParams{Amount: amount, ID: from})
	})
}

// payInterest adds a tenth to an account's balance by reading the balance into Go and
// writing back the result, under retrySerialisable.
//
// To show a conflict on demand, the first attempt deposits 100 into the same account on
// the pool between its read and its write, standing in for another client. That deposit
// commits first, so the balance this transaction read is stale. CockroachDB uses
// SERIALIZABLE isolation by default, so instead of letting the write overwrite the
// deposit it fails the write with SQLSTATE 40001, "restart transaction", and RunInTx
// rolls back. The second attempt reads the balance with the deposit in it, and commits.
//
// Takes database (*sql.DB) which begins each attempt's transaction.
// Takes queries (*store.Queries) which run the reads and writes.
// Takes account (int64) which is the account to pay.
//
// Returns []error which holds the error of each attempt CockroachDB aborted.
// Returns error when an attempt fails other than with 40001, or every attempt does.
func payInterest(ctx context.Context, database *sql.DB, queries *store.Queries, account int64) ([]error, error) {
	interrupted := false
	return retrySerialisable(ctx, database, queries, func(inTransaction *store.Queries) error {
		current, err := inTransaction.GetBalance(ctx, account)
		if err != nil {
			return err
		}
		if !interrupted {
			interrupted = true
			if err := queries.Deposit(ctx, store.DepositParams{Amount: 100, ID: account}); err != nil {
				return fmt.Errorf("depositing from another client: %w", err)
			}
		}
		return inTransaction.SetBalance(ctx, store.SetBalanceParams{Balance: current.Balance + current.Balance/10, ID: account})
	})
}

// retrySerialisable runs work with RunInTx, and runs it again in a new transaction each
// time CockroachDB aborts the transaction with SQLSTATE 40001, up to maxAttempts.
//
// The generated RunInTx commits or rolls back once and returns the error, so on
// CockroachDB a caller retries itself. work must be safe to run more than once: each
// attempt starts again from its first read, and anything work does outside the
// transaction happens once per attempt. Under heavy contention, wait a short, jittered
// while between attempts as well.
//
// Takes database (*sql.DB) which begins each transaction.
// Takes queries (*store.Queries) which RunInTx binds to it.
// Takes work (func(*store.Queries) error) which is the transaction's work.
//
// Returns []error which holds the error of each attempt CockroachDB aborted, so the
// caller can log them.
// Returns error which is nil once an attempt commits, work's own error, or the 40001 of
// the last attempt.
func retrySerialisable(ctx context.Context, database *sql.DB, queries *store.Queries, work func(*store.Queries) error) ([]error, error) {
	var aborted []error
	for range maxAttempts {
		err := queries.RunInTx(ctx, database, work)
		if sqlState(err) != serialisationFailure {
			return aborted, err
		}
		aborted = append(aborted, err)
	}
	return aborted, fmt.Errorf("still conflicting after %d attempts: %w", maxAttempts, aborted[len(aborted)-1])
}

// sqlState returns the SQLSTATE that err, or an error it wraps, carries.
//
// Takes err (error) which is the error to inspect, or nil.
//
// Returns string which is the five-character code, or empty when err has none.
func sqlState(err error) string {
	stateError, hasState := errors.AsType[sqlStateError](err)
	if !hasState {
		return ""
	}
	return stateError.SQLState()
}

// printBalances writes every account's balance on one line.
//
// Takes queries (*store.Queries) which read the accounts.
// Takes output (io.Writer) which receives the line.
//
// Returns error when the accounts cannot be listed.
func printBalances(ctx context.Context, queries *store.Queries, output io.Writer) error {
	accounts, err := queries.ListAccounts(ctx)
	if err != nil {
		return fmt.Errorf("listing accounts: %w", err)
	}
	fmt.Fprint(output, "  balances:")
	for _, account := range accounts {
		fmt.Fprintf(output, " %s %d", account.Owner, account.Balance)
	}
	fmt.Fprintln(output)
	return nil
}
