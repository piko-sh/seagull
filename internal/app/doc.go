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

// Package app is the composition root behind the seagull package: it builds a Generator
// from a dialect and the options a caller passes, and runs it.
//
// It picks every default a caller leaves out: the database/sql emitter, the system clock,
// the default preset, and the runtime tables a schema source's catalogue leaves out. A
// run without an observer reports to the one its context carries, or to none. The
// pipeline below it reads only the contracts, so this is the one place a built-in adapter
// meets the core.
//
// Besides generation it holds the helpers that work on a result or a SQL file without a
// generator: WriteFiles writes a result's files, and FormatDirectives and
// FormatDirectivesFor rewrite directives into their canonical form.
package app
