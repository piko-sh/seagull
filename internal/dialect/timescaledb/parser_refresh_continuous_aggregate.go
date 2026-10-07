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

package timescaledb

import (
	"fmt"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

// parseRefreshContinuousAggregateCall parses a CALL refresh_continuous_aggregate(name,
// start, finish) procedure invocation.
//
// TimescaleDB exposes the incremental refresh helper as a procedure (not a
// SELECT-returning function) because the refresh runs across multiple transactions;
// postgres CALL is the appropriate dispatch form. The mutation is a
// MutationAlterTableAlterColumn marker so the catalogue updates the existing
// continuous-aggregate relation rather than refusing the statement as a duplicate CREATE
// VIEW.
//
// The three captured arguments are stored verbatim in EngineSpecific
// (TIMESCALE_REFRESH_CONTINUOUS_AGGREGATE_TARGET, TIMESCALE_REFRESH_WINDOW_START,
// TIMESCALE_REFRESH_WINDOW_END) so downstream consumers can replay the call. The window
// bounds are kept opaque; both '2020-01-01' and NULL are common, and re-interpreting them
// would duplicate logic the migration runner already performs at execution time.
//
// Takes p (pgext.ParserContext) which drives the parse.
//
// Returns *ast.CatalogueMutation which describes the refresh call.
// Returns error when the statement fails to parse.
func parseRefreshContinuousAggregateCall(p pgext.ParserContext) (*ast.CatalogueMutation, error) {
	operation := funcNameRefreshContinuousAggregate
	if err := p.ExpectKeyword("CALL"); err != nil {
		return nil, err
	}
	if !p.MatchKeyword(operation) {
		return nil, dialectkit.NewParseError(p.CurrentToken(), fmt.Sprintf("expected %s", operation))
	}
	if p.CurrentToken().Kind != pgext.TokenLeftParen {
		return nil, dialectkit.NewParseError(p.CurrentToken(), fmt.Sprintf("expected '(' after %s", operation))
	}
	openParen := p.Advance()

	target, targetErr := extractFirstPolicyArgument(p)
	if targetErr != nil {
		return nil, fmt.Errorf("%s: target argument: %w", operation, targetErr)
	}
	mutation := buildRefreshContinuousAggregateMutation(target)

	if !consumeArgumentSeparator(p) {
		if closeErr := expectCallClose(p, operation, openParen); closeErr != nil {
			return nil, closeErr
		}
		if endErr := expectCallEnd(p, operation); endErr != nil {
			return nil, endErr
		}
		return mutation, nil
	}

	startArgument, startErr := captureRefreshWindowBound(p)
	if startErr != nil {
		return nil, fmt.Errorf("%s: window-start argument: %w", operation, startErr)
	}
	mutation.EngineSpecific["TIMESCALE_REFRESH_WINDOW_START"] = startArgument

	if consumeArgumentSeparator(p) {
		finishArgument, finishErr := captureRefreshWindowBound(p)
		if finishErr != nil {
			return nil, fmt.Errorf("%s: window-end argument: %w", operation, finishErr)
		}
		mutation.EngineSpecific["TIMESCALE_REFRESH_WINDOW_END"] = finishArgument
	}

	if closeErr := expectCallClose(p, operation, openParen); closeErr != nil {
		return nil, closeErr
	}
	if endErr := expectCallEnd(p, operation); endErr != nil {
		return nil, endErr
	}
	return mutation, nil
}

// buildRefreshContinuousAggregateMutation populates the catalogue mutation produced by a
// parsed CALL refresh_continuous_aggregate. The target argument is split into
// schema/table so a schema-qualified reference (`'analytics.hourly_temps'`) resolves
// through the catalogue without further work.
//
// Takes target (string) which is the first positional argument's text.
//
// Returns *ast.CatalogueMutation with EngineSpecific populated.
func buildRefreshContinuousAggregateMutation(target string) *ast.CatalogueMutation {
	schema, name := splitMaybeSchemaQualified(target)
	return ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, schema, name, ast.WithEngineSpecific(map[string]string{
		"TIMESCALE_REFRESH_CONTINUOUS_AGGREGATE_TARGET": target,
		"TIMESCALE_POLICY_OP":                           funcNameRefreshContinuousAggregate,
	}))
}

// captureRefreshWindowBound reads a single window-bound argument: a literal (string or
// numeric), a bare identifier (commonly NULL), or a multi-token expression like `INTERVAL
// '1 day'` / `NOW() - INTERVAL ...`. The capture stops at the top-level comma or close
// paren of the call.
//
// Takes p (pgext.ParserContext) which is positioned at the first token of the bound.
//
// Returns string which is the captured text, as written.
// Returns error when the bound is missing, or opens a group it does not close.
func captureRefreshWindowBound(p pgext.ParserContext) (string, error) {
	bound, err := p.CaptureUntilBoundary()
	if err != nil {
		return "", err
	}
	if bound == "" {
		return "", dialectkit.NewParseError(p.CurrentToken(), "expected a window bound")
	}
	return bound, nil
}
