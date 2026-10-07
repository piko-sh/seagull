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

// Package harness runs the integration suites under tests/integration, one module per
// engine, so every suite generates, builds and checks its cases the same way.
//
// A case is a directory under a suite's testdata holding migrations, queries, a
// testspec.json and a runner.go. Run generates the case's package with the suite's
// dialect and the emitter the spec names, checks the generation's diagnostics against the
// spec, builds the runner against the generated package and the suite's database driver
// in a module of its own, runs it against the suite's database and compares the JSON it
// prints with the case's golden/output.json.
//
// The runner opens its database through the driver the suite names: the harness writes a
// driver.go beside it that imports the driver and declares its name as the constant
// databaseDriver, so one runner can run against two drivers of the same engine.
//
// The package also holds what the suites share beyond their cases: Main for TestMain, the
// per-case schemas and databases of the server suites, the migration lifecycle the
// run-time tests drive, the examples runner, and the comparison of an introspected
// catalogue with a migration replay. It is test tooling: only the suites import it.
package harness
