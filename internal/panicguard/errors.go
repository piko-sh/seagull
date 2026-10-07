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
	"fmt"

	"seagull.sh/seagull/internal/fault"
)

// Error reports a panic recovered from a guarded call.
//
// It matches fault.ErrPanic with errors.Is, and also the panic value when that value is
// an error. It holds no stack, because the stack is logged once where the panic was
// recovered.
type Error struct {
	// Value is the value the call panicked with.
	Value any

	// Operation names the work that panicked.
	Operation string
}

// Error names the operation and the panic value.
//
// Returns string which reads "<operation>: panic: <value>".
func (e *Error) Error() string {
	return fmt.Sprintf("%s: panic: %v", e.Operation, e.Value)
}

// Unwrap returns fault.ErrPanic, followed by the panic value when that value is an error.
//
// Returns []error which holds fault.ErrPanic and, when the panic value is an error, that
// value.
func (e *Error) Unwrap() []error {
	if err, ok := e.Value.(error); ok {
		return []error{fault.ErrPanic, err}
	}
	return []error{fault.ErrPanic}
}
