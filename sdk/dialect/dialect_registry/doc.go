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

// Package dialect_registry builds the built-in dialects by name.
//
// It is the single string-keyed entry point to the dialects: the sgl command and
// embedders that read a dialect name from configuration call [New] with the name and a
// plain-data [Options] value, and [Names] lists what New accepts: postgres, cockroachdb,
// timescaledb, mysql, mariadb, sqlite, duckdb and clickhouse. Code that knows its dialect
// at compile time calls the engine provider's own New instead, such as
// dialect_engine_postgres.New, whose options set the same values.
//
// New refuses a name it does not know with [ErrUnknownDialect], and an option that is out
// of range or does not apply to the named dialect with [ErrInvalidOption]; [OptionErrors]
// lists each refused option. [DefaultMaxParseDepth] and [DefaultMaxTokensPerStatement]
// are the parser limits a dialect keeps when none is set, and [MaxParseDepthCeiling] and
// [MaxTokensPerStatementCeiling] the most either may be.
//
// # Usage
//
//	engine, err := dialect_registry.New(config.Dialect, dialect_registry.Options{
//	    SearchPath: config.SearchPath,
//	})
//	if err != nil {
//	    return err
//	}
//	generator, err := seagull.New(engine)
package dialect_registry
