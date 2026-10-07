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

// Package check runs the diagnostic passes over an analysed query.
//
// DiagnosticAnalyser runs each pass in turn over a DiagnosticContext (the analysed query,
// its raw analysis, its scope chain and parameter directives) and collects their warnings
// and errors: unknown columns and functions in the clauses of a query (its WHERE, ORDER
// BY, SET and the like), command and output mismatches, unsafe dynamic queries, writes to
// generated columns, invalid group_by and slice use, parameter count problems, and
// asynchronous execution the dialect cannot honour. What the passes read of the dialect
// is read once, when NewDiagnosticAnalyser makes them. ValidateDuplicateNames reports
// query names duplicated across files.
//
// An unknown column or function inside the select list is not a pass's concern: the type
// resolver reports each one where it is written while it types the select list, so every
// such reference is reported once, by one implementation.
package check
