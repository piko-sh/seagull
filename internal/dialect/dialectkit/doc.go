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

// Package dialectkit holds helpers for writing a dialect.
//
// These are tools, not a framework: there is no base parser and no grammar here. A
// dialect calls the helpers it wants, supplies its own decisions through predicates and
// configuration, and stays free to write everything else its own way, so the shared code
// never branches on a dialect and adopting one tool never forces another.
//
// The tools are:
//
//   - the lexical layer: LexicalRules with its comment and number rules, character
//     classes, doubled-delimiter literals, and NumberRules.ReadNumber and ClassifyNumber,
//     which scan a numeric literal (leading dot, fraction, exponent, base prefixes) and
//     classify it by shape as integer, exact or approximate, and
//     NumberRules.TypeModifierMagnitude, which reads a type modifier such as a length or
//     precision, held at MaxTypeModifier;
//   - Token, a token whose Kind stays the dialect's own, with its decoded Value, its Raw
//     source text, its Start and End offsets and whether it is a Quoted identifier;
//   - LexStatements, which cuts a script into statements under the engine's lexical rules
//     and tokenises each one with the dialect's StatementTokeniser, stopping a statement
//     at the token budget;
//   - RunOnStatement and RunOnStream, which find where a statement runs on into the next
//     one because a semicolon is missing; RunOnStream reads the tokens one at a time, so
//     a statement LexStatements stopped at the budget can still be checked whole;
//   - Cursor, which walks a token stream and matches keywords, never matching a quoted
//     identifier, and whose RequireEnd rejects tokens left after a complete statement;
//     WalkGuard gives it an Interrupt that stops a walk when the context is done or the
//     statement's one read budget, shared by every cursor over it, is spent;
//   - Limits, the parse depth and per-statement token budget, with their defaults and
//     ceilings;
//   - errors: ParseError, the one syntax-error format, wrapping the generator's parse
//     sentinel; ErrParseDepth, ErrTokenBudget, ErrWalkBudget and LimitSentinel, the limit
//     sentinels, wrapping its limit sentinel; and Recover and RecoveredError, which turn
//     a panic at a dialect's entry point into a returned error, logging its stack once,
//     and return an interrupted walk's error as it is;
//   - span helpers, which locate clauses, items and placeholders by token, and view
//     helpers, which name a view's columns by its declared list;
//   - statement scanning (ParenthesisScanIndex);
//   - a builder for built-in function catalogues (CatalogueBuilder and Arg), whose Type
//     method resolves the dialect's own type names for its type shortcuts, and
//     FunctionSpec, a catalogue written as text.
//
// The built-in dialects share an option vocabulary built from these tools: WithName, the
// limit options WithMaxParseDepth and WithMaxTokensPerStatement, WithExtraTypes,
// WithExtraFunctions taking a builder that embeds CatalogueBuilder, and the type, cast
// and promote hooks WithTypeNormaliserHook, WithImplicitCastHook and WithPromoteTypeHook.
// Type and function registrations are additive, and hooks chain, most recent first, as
// FirstHookAnswer asks them; names and limits take the last supplied value.
//
// It imports only the data model, the generator's error sentinels, its logging and the
// shared lexical helpers, so any dialect, inside this module or outside it, can use it
// without an import cycle.
package dialectkit
