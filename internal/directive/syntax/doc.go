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

// Package syntax is the lexical layer of Seagull's SQL comment directives.
//
// It holds what both the analyser and the migration run time need without the directive
// semantics. SQL text is read under a dialect's Lexicon, its sqllex rules and directive
// comment prefix, so a comment opener inside a string, a quoted identifier, a
// dollar-quoted body or a block comment is never taken for a directive: the directive
// comments of a text, the scan of a migration's directives (no_transaction, read-only and
// column overrides, misplaced query headers, foreign prefixes), StripDirectives, which
// removes directive comment lines from a query, and FormatSource, which rewrites
// directives into their canonical form. The argument grammar inside a directive call is
// Seagull's own, with its own small lexer and call-argument parser, and ValidateCall
// checks a parsed call against the arguments its directive accepts, so ParseMigration can
// report what is wrong with a migration's directives without the directive package.
package syntax
