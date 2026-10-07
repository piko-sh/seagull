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

package migrate

import (
	"context"
	"fmt"

	"seagull.sh/seagull/internal/logging"
)

// HookPanicError reports a panic recovered from a migration hook.
//
// The run stops with it as it would with an error the hook returned. Match it with
// errors.As, or match every recovered panic with errors.Is and ErrPanic. The stack is
// logged once, at warning level through the context's logger, where the panic was
// recovered; the error does not carry it.
type HookPanicError struct {
	// Value is the value the hook panicked with.
	Value any

	// Hook names the hook that panicked, such as "migration before-hook".
	Hook string
}

// Error describes the recovered panic.
//
// Returns string which names the hook and the panic value.
func (e *HookPanicError) Error() string {
	return fmt.Sprintf("%s panicked: %v", e.Hook, e.Value)
}

// Unwrap returns ErrPanic, and the panic value when it is itself an error, so errors.Is
// matches ErrPanic and errors.Is and errors.As see through a hook that panicked with an
// error.
//
// Returns []error which holds ErrPanic, then the panic value when it is an error.
func (e *HookPanicError) Unwrap() []error {
	if err, ok := e.Value.(error); ok {
		return []error{ErrPanic, err}
	}
	return []error{ErrPanic}
}

// runHooks calls each hook in registration order through callHook, stopping at the first
// one that fails.
//
// Takes hook (string) which names the hooks for errors and log records.
// Takes hooks ([]Hook) which are the registered hooks.
// Takes call (func(Hook) error) which runs one hook.
//
// Returns error which is the first hook's error or *HookPanicError, or nil.
func runHooks[Hook any](ctx context.Context, hook string, hooks []Hook, call func(Hook) error) error {
	for _, registered := range hooks {
		if err := callHook(ctx, hook, func() error { return call(registered) }); err != nil {
			return err
		}
	}
	return nil
}

// callHook runs a hook and converts a panic in it into a *HookPanicError.
//
// The stack is logged once at warning level through the context's logger, and the
// returned error carries none.
//
// Takes hook (string) which names the hook for the error and the log record.
// Takes operation (func() error) which runs the hook.
//
// Returns error which is the hook's own error, or a *HookPanicError when it panicked.
func callHook(ctx context.Context, hook string, operation func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.LogRecoveredPanic(ctx, hook, recovered)
			err = &HookPanicError{Value: recovered, Hook: hook}
		}
	}()

	return operation()
}
