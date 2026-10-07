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

package cli

import (
	"context"
	"fmt"

	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/dialect"
)

// runDialects lists every dialect this binary knows and what it supports.
//
// Takes arguments ([]string) which must be empty.
//
// Returns int which is the exit status.
func (inv *invocation) runDialects(_ context.Context, arguments []string) int {
	if status, proceed := inv.parseNoArguments("dialects", arguments); !proceed {
		return status
	}
	table := newTable(inv.streams.Stdout)
	fmt.Fprintln(table, "NAME\tFAMILY\tPARAMETERS\tRETURNING\tMIGRATE\tINTROSPECT\tDRIVER")
	for _, name := range inv.settings.dialectNames() {
		probe := &config.Database{}
		probe.Name, probe.Dialect = name, name
		built, err := inv.buildDialect(probe)
		if err != nil {
			return inv.fail(err)
		}
		info := built.Info()
		driver, driverErr := inv.driverFor(probe, info.Family)
		if driverErr != nil {
			driver = "none"
		}
		_, migrates := migrationBackendFor(&info, inv.settings.backends)
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", name, info.Family, info.Placeholders.Execution.Render(1, "name", "Type"),
			returningStatements(info.Returning), yesNo(migrates), yesNo(hasIntrospection(info.Family)), driver)
	}
	_ = table.Flush()
	return exitOK
}

// returningStatements prints the statements a dialect accepts a RETURNING clause on.
//
// Takes statements (dialect.DataChange) which is the set.
//
// Returns string which lists the statements, such as "INSERT, DELETE", or is no when the
// set is empty.
func returningStatements(statements dialect.DataChange) string {
	if statements == 0 {
		return "no"
	}
	return statements.String()
}

// yesNo prints a capability flag.
//
// Takes value (bool) which is the flag.
//
// Returns string which is yes or no.
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
