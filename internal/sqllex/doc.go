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

// Package sqllex is Seagull's lexical layer: one description of how an engine writes SQL,
// a scanner that reads text under that description, and a statement splitter built on the
// scanner.
//
// An engine declares its lexical facts once as a Rules value: how quotes and escapes
// work, which comment forms exist, which parameter sigils it uses, its statement
// terminator, whether a client-side DELIMITER command can change that terminator, and
// which statements hold procedural BEGIN ... END bodies. Everything else in sqllex reads
// those rules and never branches on an engine, so the same scanner serves every engine
// and a new engine only writes a Rules value.
//
// Scanner turns text into a stream of tokens (words, quoted identifiers, strings,
// numbers, parameters, punctuation, comments and whitespace), each carrying its byte span
// and raw text. Split cuts a script into statements with their source spans, so the code
// generator and the migration runner agree on where each statement begins and ends.
//
// The package imports only the standard library. A migration runner can therefore link an
// engine's rules without linking a parser, a data model or anything else in the module.
//
// Input is UTF-8. A byte order mark at the start of the text is read as whitespace, and
// lexical errors carry the byte offset, line and rune column of the construct that
// failed, wrapping both a specific sentinel (such as ErrUnterminatedString) and
// ErrMalformed.
package sqllex
