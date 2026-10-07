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

// Package emit_testkit checks a code emitter: a conformance suite of the behaviours every
// emitter must share, whatever driver API it writes Go for and whatever dialect the
// queries come from. Every built-in emitter passes it.
//
// [Run] drives an emitter through the generator with every dialect a [Suite] names, over
// a neutral corpus the suite writes in each dialect's own syntax, so it never names an
// engine. It adds the martian dialect, an invented engine that shares no convention with
// a real database, so every emitter is shown to assume no real engine. Each case of the
// corpus is one generated package, held to the guarantees that keep emitters
// interchangeable: generation fails only with diagnostics, files are named safely and
// carry the header, the output type-checks, every parameter is bound and every row field
// filled, generated names never collide, and the output is the same however the queries
// are ordered. A [Corpus] adds case directories written in one dialect's SQL, such as a
// dialect's own test data.
//
// A [Harness] adds the runtime lane: the suite builds the generated packages with a
// recording stand-in for the driver, calls every method of the runtime cases and checks
// what the driver receives. [DatabaseSQL] is the harness for code written against
// database/sql; a Harness of an emitter's own stands in for any other driver API.
//
// A check an emitter fails today is recorded in Suite.KnownFailures with its reason, and
// must keep failing until the entry is removed. The checks that need the go command, the
// type checks and the runtime lane, are skipped under -short. The package uses only the
// standard library's testing package, so importing it adds no test dependencies.
//
// # Usage
//
//	func TestConformance(t *testing.T) {
//	    emit_testkit.Run(t, emit_testkit.Suite{
//	        Name: "my-emitter",
//	        New: func(*dialect.Facts) emit.EmitterPort {
//	            return myemitter.New()
//	        },
//	        Dialects: []dialect.Dialect{
//	            dialect_engine_sqlite.New(),
//	            dialect_engine_postgres.New(),
//	        },
//	        Harness: emit_testkit.DatabaseSQL(),
//	    })
//	}
package emit_testkit
