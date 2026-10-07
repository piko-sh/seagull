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

// Package bridge holds everything the playground's WebAssembly entry point does that does
// not need JavaScript: reading each call's arguments, decoding requests, running the
// session package and encoding the answer as JSON.
//
// The entry point only adapts JavaScript values to [Argument] and registers each function
// [Names] lists, so the size checks and the shape of every answer are tested natively.
//
// An argument is measured with [Argument.Length] before it is copied into Go, and one
// longer than its function reads is refused, so a page cannot make the playground copy
// and decode a request of any size. [Call] never panics: a panic becomes an internal
// failure, and its stack is logged once, at warning level, through the logger the call's
// context carries, or slog.Default() when it carries none, which under WebAssembly writes
// to the console.
package bridge
