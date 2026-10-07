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

package dialecttest

import (
	"context"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// lateralAliasProbe names, in its second select item, the alias its first item gives an
	// expression of the fixture's items table.
	lateralAliasProbe = "SELECT id + 1 AS bumped, bumped * 2 AS doubled FROM items;"

	// lateralAlias is the alias lateralAliasProbe reuses.
	lateralAlias = "bumped"
)

// checkSelectListAliasesFollowInfo checks alias visibility within the select list.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkSelectListAliasesFollowInfo(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{"lateral.sql": directiveLine(engine, "Lateral", "many") + lateralAliasProbe})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	reported := slices.ContainsFunc(result.Diagnostics, func(diagnostic diag.SourceError) bool {
		return diagnostic.Code == diag.CodeUnknownColumn && quotesName(diagnostic.Message, lateralAlias)
	})
	if !engine.Info().SelectAliases.Includes(ast.ClauseSelectList) {
		if !reported {
			return failed("%q names the alias %s in a later select item, which Info.SelectAliases does not make visible there, "+
				"so it must be reported as an unknown column, got %s", lateralAliasProbe, lateralAlias, formatDiagnostics(result.Diagnostics))
		}
		return passed
	}
	if len(result.Diagnostics) > 0 {
		return failed("%q must analyse without diagnostics where Info.SelectAliases holds ast.ClauseSelectList, got %s",
			lateralAliasProbe, formatDiagnostics(result.Diagnostics))
	}
	query := findQuery(emitter.queries, "Lateral")
	if query == nil || len(query.OutputColumns) != 2 {
		return failed("the query Lateral was not analysed into two columns")
	}
	bumped, doubled := &query.OutputColumns[0], &query.OutputColumns[1]
	if doubled.SQLType.Category == sqlmodel.TypeCategoryUnknown || doubled.SQLType.Category != bumped.SQLType.Category ||
		doubled.Nullable != bumped.Nullable {
		return failed("the item naming the alias %s is %s, nullable %v, but must be typed from the item the alias names, %s, "+
			"nullable %v", lateralAlias, describeType(&doubled.SQLType), doubled.Nullable, describeType(&bumped.SQLType), bumped.Nullable)
	}
	return passed
}
