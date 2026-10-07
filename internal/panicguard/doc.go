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

// Package panicguard turns a panic in code Seagull calls into an error.
//
// A guarded call that panics returns an *Error naming the operation and holding the panic
// value. The error matches fault.ErrPanic with errors.Is, and the panic value too when
// that value is an error. The stack is logged once, at warning level through the
// context's logger, where the panic was recovered, and never travels on the error.
//
// A panic value that implements InterruptError() error is a controlled stop rather than a
// fault, such as a budget a parser trips to end a runaway walk. The guard returns the
// interrupt's error and logs nothing.
//
// The package imports only the standard library, internal/logging and internal/fault, so
// every layer can guard its calls with it, the migration runner included.
package panicguard
