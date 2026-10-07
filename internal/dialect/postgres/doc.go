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

// Package postgres is the dialect for PostgreSQL and PostgreSQL-compatible databases.
//
// It uses a hand-written recursive-descent parser to convert DDL into catalogue mutations
// and analyse DML queries for type resolution. Its options let flavours such as
// CockroachDB and TimescaleDB customise types, functions and semantic rules without
// forking the parser. Registration options are additive; value-setting options can
// override a flavour's defaults (see Option). The extension interface in package pgext
// lets a flavour recognise statements PostgreSQL does not have.
package postgres
