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

// Package lexical declares TimescaleDB's lexical rules for the shared scanner and
// statement splitter in seagull.sh/seagull/internal/sqllex.
//
// TimescaleDB writes SQL exactly as PostgreSQL does, so the rules are PostgreSQL's from
// seagull.sh/seagull/internal/dialect/postgres/lexical, with one addition: statements
// that create a continuous aggregate with data or refresh one cannot run inside a
// transaction. The package imports only sqllex and PostgreSQL's lexical rules, so the
// migration runner can use it without linking a parser.
package lexical
