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
	"math"
)

const (
	// interruptInterval is how many reads of the stream a WalkGuard counts between two
	// checks of its context and its budget.
	interruptInterval = 1 << 12

	// walkReadsFloor is the fewest reads WalkGuard allows a walk, whatever the length of its
	// statement.
	walkReadsFloor = 1 << 24
)

// walkInterrupted is the value a Cursor panics with when its Interrupt stops a walk,
// which Recover and RecoveredError turn back into the error the Interrupt returned.
//
// It is a value rather than a pointer, so a recovered interrupt is never a nil pointer.
type walkInterrupted struct {
	// err is the error the Interrupt returned.
	err error
}

// InterruptError returns the error the Interrupt stopped the walk with, which marks the
// value as a controlled stop rather than a fault to a guard that recovers it.
//
// Returns error which is the Interrupt's error.
func (interrupted walkInterrupted) InterruptError() error {
	return interrupted.err
}

// WalkGuard creates a cursor interrupt that limits the walk of one statement's parse.
//
// The interrupt counts every read of the stream by every Cursor it is given to, so a
// parser that starts child cursors over parts of the same statement gives them the
// parent's interrupt and the whole statement shares one budget. Every interruptInterval
// reads it checks the context, returning its cause once it is done, and the budget,
// returning ErrWalkBudget once the reads pass walkReadsFloor plus the square of the
// statement's token count. Once spent, the budget stays spent.
//
// The interrupt keeps its count without locking, so it serves one goroutine.
//
// Takes tokens (int) which is the number of tokens in the statement.
//
// Returns func() error which is the Interrupt.
func WalkGuard(ctx context.Context, tokens int) func() error {
	return newReadBudget(ctx, walkReadLimit(tokens))
}

// newReadBudget creates an interrupt that stops a walk once the context is done or the
// reads pass a limit, checking both every interruptInterval reads.
//
// Takes limit (int) which is the most reads the walk may make.
//
// Returns func() error which is the Interrupt.
func newReadBudget(ctx context.Context, limit int) func() error {
	reads := 0
	return func() error {
		reads++
		if reads%interruptInterval != 0 {
			return nil
		}
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if reads > limit {
			return ErrWalkBudget
		}
		return nil
	}
}

// walkReadLimit returns the most reads a walk of a statement may make: walkReadsFloor
// plus the square of its token count, held at math.MaxInt rather than overflowing.
//
// Takes tokens (int) which is the number of tokens in the statement.
//
// Returns int which is the limit.
func walkReadLimit(tokens int) int {
	if tokens <= 0 {
		return walkReadsFloor
	}
	if tokens > math.MaxInt/tokens {
		return math.MaxInt
	}
	squared := tokens * tokens
	if squared > math.MaxInt-walkReadsFloor {
		return math.MaxInt
	}
	return walkReadsFloor + squared
}
