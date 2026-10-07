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

// Package pgext is the extension interface of the postgres dialect: what a PostgreSQL
// flavour built in this module, such as TimescaleDB, uses to recognise and parse
// statements PostgreSQL itself does not have.
//
// A flavour passes a StatementExtensionPort to postgres.WithStatementExtensions and a
// PostParseHook to postgres.WithPostParseHook. The dialect hands each one the statement's
// tokens and a ParserContext that drives its own parsing helpers, so the flavour never
// re-implements the tokeniser or the column-type parser.
//
// The interface is internal and may change with the flavours that use it. Token kinds
// have fixed values, so adding a kind never renumbers the existing ones.
package pgext
