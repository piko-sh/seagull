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

// Package martian is an invented SQL engine with distinct syntax and type names, used to
// prove that the generator's core treats no engine as the reference.
//
// Martian folds unquoted identifiers to upper case, delimits identifiers with square
// brackets ([weird name]), writes parameters as @p1, @p2 and so on, resolves unqualified
// names in the dbo schema (DBO once folded, so dbo.items resolves), has no || operator
// (strings join through the CONCAT function), and spells its types WHOLE16, WHOLE32,
// WHOLE64, REAL32, REAL64, EXACT, TEXTUAL, FLAG, MOMENT, DAYSTAMP and BLOB.
//
// The grammar is deliberately small: CREATE TABLE with column types, NOT NULL, DEFAULT
// and PRIMARY KEY; SELECT from one table with a WHERE clause and ORDER BY; INSERT with a
// VALUES row; UPDATE with SET and WHERE; and DELETE with WHERE. It is a real dialect
// nonetheless, implementing dialect.DialectPort through the dialect contract alone,
// without borrowing any other dialect's grammar, so a core assumption that only holds for
// the built-in engines shows up as a failure here. Like a built-in dialect, it stops with
// the context's cause once the context is done, refuses another dialect's statement with
// dialect.ErrForeignStatement, and recovers a panic at each method that takes a context
// through dialectkit.Recover.
//
// Its syntax errors are dialectkit.ParseError values, so they match the generator's parse
// sentinel and say where in the text the problem is.
//
// It is test tooling: emittest.Run adds it to every emitter's suite, so each emitter,
// built in or written outside this module, is held to the same guard, and the dialect
// conformance suite runs over it, the checks its small grammar cannot express recorded as
// known failures. No production package imports it; import it only from tests.
package martian
