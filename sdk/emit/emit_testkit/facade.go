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

package emit_testkit

import (
	"testing"

	"seagull.sh/seagull/internal/emit/emittest"
)

// The suite Run checks an emitter with.
type (
	// Suite describes an emitter for Run: how to build it, the dialects it generates for,
	// the optional runtime harness, and the checks it is known to fail today.
	Suite = emittest.Suite

	// Corpus describes a tree of dialect-specific generation test cases.
	//
	// Each case supplies a spec, migrations and queries. Skipped cases and cases producing
	// errors are excluded from checks of generated code.
	Corpus = emittest.Corpus

	// Harness is the runtime lane's stand-in for the driver an emitter writes Go for. The
	// suite builds the generated packages into a program with it, calls every method of the
	// runtime cases, and examines the statements the stand-in received.
	//
	// Source is one Go file, compiled into the program's package main whatever package
	// clause it carries, that declares:
	//
	// 	type Statement struct {
	// 		SQL       string
	// 		Arguments []any
	// 	}
	//
	// 	func Connect() T
	// 	func Take() []Statement
	//
	// Connect returns the connection the generated package's New takes. Take returns the
	// statements the stand-in received since it was last called, in order, and forgets them;
	// each argument is as the generated code passed it, a named one as a sql.NamedArg or a
	// driver.NamedValue. The stand-in answers every query with no rows.
	Harness = emittest.Harness
)

// DatabaseSQL returns the harness for code written against database/sql: a driver that
// records every statement and its arguments as the generated code passed them.
//
// Returns *Harness which needs no module beside the standard library.
func DatabaseSQL() *Harness {
	return emittest.DatabaseSQL()
}

// Run checks an emitter's generation and runtime contracts for each dialect in the suite,
// and for the martian dialect, an invented engine that shares no convention with a real
// database, which it adds unless the suite lists it. Call it from the emitter's own test.
//
// Checks run as subtests. KnownFailures entries must still fail; unexpected failures fail
// the suite, and an entry that gives no reason, or names no check or no dialect the suite
// drives, stops the test before any check runs. A check that panics fails with the
// panic's value, and its stack is logged once.
//
// Takes t (*testing.T) which reports test failures.
// Takes suite (Suite) which describes the emitter and the dialects to drive it with.
func Run(t *testing.T, suite Suite) {
	emittest.Run(t, suite)
}
