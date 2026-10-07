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

// Command sgl is the Seagull command-line tool: it turns SQL migrations and queries into
// type-safe Go, runs migrations and seeds, and checks SQL for problems.
//
// Usage:
//
//	sgl <command> [flags] [args]
//
// Run `sgl help` for the full command reference.
//
// The stock binary links the pure-Go database drivers for PostgreSQL, MySQL, SQLite and
// ClickHouse. To build a customised sgl binary with extra dialects, emitters or database
// drivers, depend on seagull.sh/seagull/cmd/sgl/cli and write your own main that calls
// cli.Main with the options you want; the cli package itself links no driver.
//
// The seagull command, seagull.sh/seagull/cmd/seagull, is this tool under the project's
// name.
package main

import (
	"os"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/drivers"
)

// exit ends the process with a status code; tests stand in for it.
var exit = os.Exit

// main runs the sgl CLI with the stock drivers and exits with the returned status code.
func main() {
	exit(cli.Main(os.Args[1:], append(drivers.Options(), cli.WithName("sgl"))...))
}
