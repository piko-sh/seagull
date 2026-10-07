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

package panicguard

import (
	"context"

	"seagull.sh/seagull/internal/logging"
)

// interrupter is a panic value that stops work on purpose and carries the error the work
// ends with, in place of a fault.
type interrupter interface {
	// InterruptError returns the error the interrupted work ends with.
	//
	// Returns error which is the interrupt's error.
	InterruptError() error
}

// Run calls call and returns its error, turning a panic into an error.
//
// Takes operation (string) which names the work for the log record and the error.
// Takes call (func() error) which is the work to guard.
//
// Returns error which is call's own error, or what Recovered returns for the value call
// panicked with.
func Run(ctx context.Context, operation string, call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = Recovered(ctx, operation, recovered)
		}
	}()
	return call()
}

// RunValue calls call and returns its result and error, turning a panic into an error.
//
// Takes operation (string) which names the work for the log record and the error.
// Takes call (func() (T, error)) which is the work to guard.
//
// Returns T which is call's result, or the zero value when call panicked.
// Returns error which is call's own error, or what Recovered returns for the value call
// panicked with.
func RunValue[T any](ctx context.Context, operation string, call func() (T, error)) (result T, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = Recovered(ctx, operation, recovered)
		}
	}()
	return call()
}

// Recovered turns a value recovered from a panic into the error to return.
//
// It is the shared step for code that recovers in its own deferred function, and is
// called from that function so the logged stack still holds the frames that panicked. A
// nil value means nothing panicked. An interrupt is returned as its own error and is not
// logged. Any other value is logged once with its stack and returned as an *Error without
// one. An interrupt whose error is nil is treated the same way, because work that stopped
// carries an error.
//
// Takes operation (string) which names the work for the log record and the error.
// Takes recovered (any) which is the value recover returned.
//
// Returns error which is nil for a nil value, the interrupt's error for an interrupt, and
// an *Error otherwise.
func Recovered(ctx context.Context, operation string, recovered any) error {
	if recovered == nil {
		return nil
	}
	if interrupt, ok := recovered.(interrupter); ok {
		if err := interrupt.InterruptError(); err != nil {
			return err
		}
	}
	logging.LogRecoveredPanic(ctx, operation, recovered)
	return &Error{Value: recovered, Operation: operation}
}
