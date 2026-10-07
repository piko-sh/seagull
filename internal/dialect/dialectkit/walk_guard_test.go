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

package dialectkit

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/fault"
)

const wordKind = 1

var errUserInterrupted = errors.New("the user interrupted generation")

func walkForever(ctx context.Context, cursor *Cursor[int]) (err error) {
	defer func() { Recover[any](ctx, "walk", recover(), nil, &err) }()
	for {
		cursor.Current()
	}
}

func readTimes(ctx context.Context, cursor *Cursor[int], reads int) (err error) {
	defer func() { Recover[any](ctx, "walk", recover(), nil, &err) }()
	for range reads {
		cursor.Current()
	}
	return nil
}

func TestWalkGuardStopsAWalkWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errUserInterrupted)
	cursor := NewCursor([]Token[int]{{Kind: wordKind, Value: "a"}}, wordKind, 0)
	cursor.Interrupt = WalkGuard(ctx, 1)

	err := walkForever(t.Context(), &cursor)

	require.ErrorIs(t, err, errUserInterrupted)
	require.NotErrorIs(t, err, fault.ErrPanic)
}

func TestWalkGuardStopsAWalkThatStopsAdvancing(t *testing.T) {
	t.Parallel()

	cursor := NewCursor([]Token[int]{{Kind: wordKind, Value: "a"}}, wordKind, 0)
	cursor.Interrupt = WalkGuard(t.Context(), 1)

	err := walkForever(t.Context(), &cursor)

	require.ErrorIs(t, err, ErrWalkBudget)
	require.ErrorIs(t, err, fault.ErrLimit)
	require.NotErrorIs(t, err, fault.ErrPanic)
}

func TestWalkGuardLetsAWalkFinish(t *testing.T) {
	t.Parallel()

	tokens := make([]Token[int], 1000)
	for index := range tokens {
		tokens[index] = Token[int]{Kind: wordKind, Value: "a"}
	}
	cursor := NewCursor(tokens, wordKind, 0)
	cursor.Interrupt = WalkGuard(t.Context(), len(tokens))
	for !cursor.AtEnd() {
		for offset := range 100 {
			cursor.PeekAt(offset)
		}
		cursor.Advance()
	}
	require.True(t, cursor.AtEnd())
}

func TestReadBudgetSharedByTwoCursorsTripsAtTheStatementWideLimit(t *testing.T) {
	t.Parallel()

	tokens := []Token[int]{{Kind: wordKind, Value: "a"}}
	limit := 3 * interruptInterval
	interrupt := newReadBudget(t.Context(), limit)
	parent := NewCursor(tokens, wordKind, 0)
	parent.Interrupt = interrupt
	child := NewCursor(tokens, wordKind, 0)
	child.Interrupt = interrupt

	require.NoError(t, readTimes(t.Context(), &parent, 2*interruptInterval))
	require.NoError(t, readTimes(t.Context(), &child, interruptInterval),
		"the reads so far reach the limit without passing it")
	require.ErrorIs(t, readTimes(t.Context(), &child, interruptInterval), ErrWalkBudget,
		"the child passes the limit the two cursors share, though it read less than the limit itself")
	require.ErrorIs(t, readTimes(t.Context(), &parent, interruptInterval), ErrWalkBudget,
		"the spent budget stays spent for the parent too")
}

func TestWalkGuardChildrenShareTheirParentsBudget(t *testing.T) {
	t.Parallel()

	tokens := []Token[int]{{Kind: wordKind, Value: "a"}}
	interrupt := WalkGuard(t.Context(), len(tokens))
	cursors := make([]Cursor[int], 4)
	for index := range cursors {
		cursors[index] = NewCursor(tokens, wordKind, 0)
		cursors[index].Interrupt = interrupt
	}
	share := walkReadLimit(len(tokens))/len(cursors) + interruptInterval

	var err error
	for index := range cursors {
		if err = readTimes(t.Context(), &cursors[index], share); err != nil {
			break
		}
	}

	require.ErrorIs(t, err, ErrWalkBudget)
}

func TestWalkReadLimitSaturatesInsteadOfOverflowing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		tokens int
		want   int
	}{
		{name: "no tokens", tokens: 0, want: walkReadsFloor},
		{name: "a negative count", tokens: -5, want: walkReadsFloor},
		{name: "one token", tokens: 1, want: walkReadsFloor + 1},
		{name: "a thousand tokens", tokens: 1_000, want: walkReadsFloor + 1_000_000},
		{name: "a square past the largest int", tokens: math.MaxInt / 2, want: math.MaxInt},
		{name: "the largest square that fits", tokens: 3_037_000_499, want: walkReadsFloor + 3_037_000_499*3_037_000_499},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, walkReadLimit(testCase.tokens))
		})
	}
}

func TestWalkInterruptedIsAValueThatCarriesItsError(t *testing.T) {
	t.Parallel()

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		cursor := NewCursor([]Token[int]{{Kind: wordKind}}, wordKind, 0)
		cursor.Interrupt = func() error { return ErrWalkBudget }
		cursor.Current()
	}()

	interrupt, isValue := recovered.(walkInterrupted)
	require.True(t, isValue, "the interrupt is a value, never a pointer that could be nil")
	assert.Equal(t, ErrWalkBudget, interrupt.InterruptError())
}
