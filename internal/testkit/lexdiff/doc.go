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

// Package lexdiff compares the shared lexical layer in seagull.sh/seagull/internal/sqllex
// with the dialect tokenisers, statement parsers and migration backends over the SQL
// inputs collected by Corpus.
//
// Corpus gathers an engine's migration, query and compatibility inputs, over which each
// dialect's internal test holds its own tokeniser to the shared scanner with
// dialecttest.CompareTokeniserSpans, since its tokeniser is unexported. The package's own
// tests check that each dialect's ParseStatements, which splits with sqllex under the
// engine's lexical leaf, reports exactly sqllex.Split's statement spans and lexical
// errors, and that each migration backend in seagull.sh/seagull/internal/migrate/sqlexec
// runs exactly the statements sqllex.Split finds, with no intended differences.
//
// It is test support only and imports nothing from the module but sqllex.
package lexdiff
