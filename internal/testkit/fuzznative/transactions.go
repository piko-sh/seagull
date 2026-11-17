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

package fuzznative

import (
	"context"
	"errors"
	"reflect"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

const (
	// transactionIdentifier lies beyond every ordinary input row.
	transactionIdentifier = 9

	// closedTransactionChecks counts the transactions whose receivers must refuse calls.
	closedTransactionChecks = 2
)

// transactions records intermediate visibility, rollback and committed persistence.
//
// Returns error when a required native transaction operation fails.
func (state *execution) transactions(ctx context.Context) error {
	if !state.model.Transactions {
		return nil
	}
	if state.session.Begin == nil {
		return errors.New("required transaction factory is absent")
	}
	var err error
	state.response.BeforeRollback, err = state.transaction(ctx, rollbackTransaction)
	if err != nil {
		return err
	}
	state.response.AfterRollback, err = state.read(ctx, state.queries, state.readMethod())
	if err != nil {
		return err
	}
	state.response.BeforeCommit, err = state.transaction(ctx, commitTransaction)
	if err != nil {
		return err
	}
	state.response.AfterCommit, err = state.read(ctx, state.queries, state.readMethod())
	return err
}

// transaction inserts and observes one sentinel through a generated transaction receiver.
//
// Takes end (func(*Transaction) func(context.Context) error) which picks Commit or
// Rollback.
//
// Returns []Row which records actual rows visible inside the transaction.
// Returns error when lifecycle operations or closed-transaction rejection fail.
func (state *execution) transaction(ctx context.Context, end func(*Transaction) func(context.Context) error) ([]Row, error) {
	transaction, err := state.session.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if !transaction.complete() {
		return nil, errors.New("native transaction factory returned incomplete operations")
	}
	defer releaseTransaction(ctx, transaction)
	receiver := reflect.ValueOf(transaction.Queries)
	if err := state.insertValue(ctx, receiver, transactionIdentifier, fuzzcase.RuntimeTransactionValue(state.model)); err != nil {
		return nil, err
	}
	rows, err := state.read(ctx, receiver, state.readMethod())
	if err != nil {
		return nil, err
	}
	if err := end(transaction)(ctx); err != nil {
		return nil, err
	}
	if _, err := state.read(ctx, receiver, state.readMethod()); err == nil {
		return nil, errors.New("completed generated transaction still executes queries")
	}
	state.response.ClosedTransactions++
	return rows, nil
}

// cancellation requires generated calls to preserve cancellation and recover afterwards.
//
// Returns error when cancellation is lost or the next ordinary query fails.
func (state *execution) cancellation(ctx context.Context) error {
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err := call(state.queries.MethodByName(state.readMethod()), reflect.ValueOf(cancelled))
	if !errors.Is(err, context.Canceled) {
		return errors.New("generated read did not preserve context cancellation")
	}
	state.response.Cancelled = true
	state.response.AfterCancel, err = state.read(ctx, state.queries, state.readMethod())
	return err
}

// rollbackTransaction picks a transaction's Rollback.
//
// Takes transaction (*Transaction) which is the open transaction.
//
// Returns func(context.Context) error which discards its writes.
func rollbackTransaction(transaction *Transaction) func(context.Context) error {
	return transaction.Rollback
}

// commitTransaction picks a transaction's Commit.
//
// Takes transaction (*Transaction) which is the open transaction.
//
// Returns func(context.Context) error which persists its writes.
func commitTransaction(transaction *Transaction) func(context.Context) error {
	return transaction.Commit
}

// releaseTransaction rolls back a transaction that an early failure left open.
//
// The rollback error is ignored on purpose. After Commit or Rollback every driver reports
// the transaction as already done, and an earlier failure is already reported.
//
// Takes transaction (*Transaction) which may still be open.
func releaseTransaction(ctx context.Context, transaction *Transaction) {
	release, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), nativeInputBound, errInputBound)
	defer cancel()
	_ = transaction.Rollback(release)
}
