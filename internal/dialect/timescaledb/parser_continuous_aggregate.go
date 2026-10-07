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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

var (
	// timescaleReloptionMap maps a lower-cased `timescaledb.*` reloption key to its
	// TIMESCALE_* EngineSpecific key.
	//
	// Centralising the mapping as a package-level table keeps the registration declarative;
	// adding a new reloption is a single map entry rather than another case arm. Keys are
	// stored lower-cased so the lookup helper can compare without allocating a normalised
	// key on every call.
	//
	// The map covers both legacy compression keys (`timescaledb.compress`,
	// `timescaledb.compress_segmentby`, and similar) and the TS 2.18+ canonical columnstore
	// keys (`timescaledb.enable_columnstore`, `timescaledb.segmentby`,
	// `timescaledb.orderby`) so reloption bodies produced by either generation of the
	// toolkit lift into the same EngineSpecific surface. Downstream consumers should treat
	// the columnstore and compression keys as semantically equivalent; the legacy keys are
	// preserved verbatim where they appeared in the SQL.
	timescaleReloptionMap = map[string]string{
		"timescaledb.continuous":                   "TIMESCALE_CONTINUOUS_FLAG",
		"timescaledb.materialized_only":            "TIMESCALE_MATERIALIZED_ONLY",
		"timescaledb.create_group_indexes":         "TIMESCALE_CREATE_GROUP_INDEXES",
		"timescaledb.finalized":                    "TIMESCALE_FINALIZED",
		"timescaledb.compress":                     "TIMESCALE_COMPRESSION_ENABLED",
		"timescaledb.compress_segmentby":           "TIMESCALE_COMPRESSION_SEGMENTBY",
		"timescaledb.compress_orderby":             "TIMESCALE_COMPRESSION_ORDERBY",
		"timescaledb.compress_chunk_time_interval": "TIMESCALE_COMPRESSION_CHUNK_TIME_INTERVAL",
		"timescaledb.enable_columnstore":           "TIMESCALE_COLUMNSTORE_ENABLED",
		"timescaledb.segmentby":                    "TIMESCALE_SEGMENTBY",
		"timescaledb.orderby":                      "TIMESCALE_ORDERBY",
	}
)

// parseCreateContinuousAggregate parses a CREATE MATERIALIZED VIEW continuous-aggregate
// statement into a catalogue mutation.
//
// The recognised shape is CREATE MATERIALIZED VIEW [IF NOT EXISTS] [schema.]name WITH
// (timescaledb.continuous = true [, timescaledb.materialized_only = ...]) AS SELECT ...
// optionally followed by WITH [NO] DATA. The reloption body is parsed structurally so the
// timescaledb keys flow into EngineSpecific. The body is analysed as CREATE VIEW analyses
// it, which types the aggregate's columns, and is also kept as written under
// TIMESCALE_VIEW_BODY.
//
// Takes p (pgext.ParserContext) which is the parser context positioned at the CREATE
// keyword.
//
// Returns *ast.CatalogueMutation which is a MutationCreateView carrying the
// continuous-aggregate marker in EngineSpecific.
// Returns error when the statement cannot be parsed.
func parseCreateContinuousAggregate(p pgext.ParserContext) (*ast.CatalogueMutation, error) {
	if err := p.ExpectKeyword("CREATE"); err != nil {
		return nil, err
	}
	if p.MatchKeyword("OR") {
		if err := p.ExpectKeyword("REPLACE"); err != nil {
			return nil, err
		}
	}
	if err := p.ExpectKeyword("MATERIALIZED"); err != nil {
		return nil, err
	}
	if err := p.ExpectKeyword("VIEW"); err != nil {
		return nil, err
	}

	ifNotExists := p.MatchIfNotExists()

	schema, name, err := p.ParseQualifiedName()
	if err != nil {
		return nil, fmt.Errorf("continuous aggregate name: %w", err)
	}

	mutation := ast.NewCatalogueMutation(ast.MutationCreateView, schema, name, ast.WithEngineSpecific(map[string]string{
		"TIMESCALE_CONTINUOUS_AGGREGATE": literalTrue,
	}))
	if ifNotExists {
		mutation.EngineSpecific["TIMESCALE_IF_NOT_EXISTS"] = literalTrue
	}

	if applyErr := applyContinuousAggregateReloptions(p, mutation, name); applyErr != nil {
		return nil, applyErr
	}

	if !p.MatchKeyword("AS") {
		return nil, dialectkit.NewParseError(p.CurrentToken(), fmt.Sprintf("continuous aggregate %q: missing AS clause", name))
	}

	viewDefinition, err := p.AnalyseViewBody(nil)
	if err != nil {
		return nil, fmt.Errorf("continuous aggregate %q: %w", name, err)
	}
	mutation.ViewDefinition = viewDefinition

	body, withData := captureViewBody(p)
	if body != "" {
		mutation.EngineSpecific["TIMESCALE_VIEW_BODY"] = body
	}
	mutation.EngineSpecific["TIMESCALE_WITH_DATA"] = withData
	return mutation, nil
}

// applyContinuousAggregateReloptions consumes the mandatory WITH body of a continuous
// aggregate.
//
// It maps known timescaledb keys onto the TIMESCALE_* EngineSpecific entries and
// preserves unknown keys verbatim under TIMESCALE_RELOPTION_<KEY>. The fallback mirrors
// the compression-alter parser so downstream consumers can still inspect keys this parser
// does not recognise.
//
// Takes p (pgext.ParserContext) which is the parser context positioned just after the
// view name.
// Takes mutation (*ast.CatalogueMutation) which has the outgoing EngineSpecific map
// populated.
// Takes name (string) which is the view name; used in error wrapping so callers see which
// statement failed.
//
// Returns error when the WITH clause is missing or the reloption body cannot be parsed.
func applyContinuousAggregateReloptions(p pgext.ParserContext, mutation *ast.CatalogueMutation, name string) error {
	if !p.MatchKeyword("WITH") {
		return dialectkit.NewParseError(p.CurrentToken(), fmt.Sprintf("continuous aggregate %q: missing WITH clause", name))
	}
	reloptions, reloptionErr := p.ParseReloptionList()
	if reloptionErr != nil {
		return fmt.Errorf("continuous aggregate %q: WITH body: %w", name, reloptionErr)
	}
	for key, value := range reloptions {
		if matched := timescaleReloptionToEngineSpecific(key); matched != "" {
			mutation.EngineSpecific[matched] = value
			continue
		}
		mutation.EngineSpecific["TIMESCALE_RELOPTION_"+strings.ToUpper(key)] = value
	}
	return nil
}

// timescaleReloptionToEngineSpecific maps a `timescaledb.*` reloption key to its
// TIMESCALE_* EngineSpecific key.
//
// Takes key (string) which is the raw reloption key from the WITH body.
//
// Returns string which is the matching TIMESCALE_* key, or "" when the key is unknown and
// is dropped by the caller's fallback path.
func timescaleReloptionToEngineSpecific(key string) string {
	return timescaleReloptionMap[strings.ToLower(key)]
}

// captureViewBody consumes the rest of a continuous aggregate after AS and returns its
// body as written, without the WITH [NO] DATA clause that may end the statement.
//
// The clause is found at the end of the statement, so a body opening with a CTE named
// data or no, as in AS WITH data AS (...) SELECT ..., keeps its WITH.
//
// Takes p (pgext.ParserContext) which is positioned at the first token of the view body
// (just after `AS`).
//
// Returns body (string) which is the body text, trimmed, or empty when there is none.
// Returns withData (string) which is literalFalse for WITH NO DATA, and literalTrue for
// WITH DATA or when the clause is absent, as TimescaleDB creates the aggregate with its
// data by default.
func captureViewBody(p pgext.ParserContext) (body string, withData string) {
	tokens := p.Tokens()
	current := p.CurrentToken()
	start := slices.IndexFunc(tokens, func(candidate pgext.Token) bool {
		return candidate.Start == current.Start && candidate.Kind == current.Kind
	})
	p.ConsumeRemainder()
	if start < 0 {
		return "", literalTrue
	}
	end, withData := trailingDataModifier(tokens, start)
	body = strings.TrimSpace(p.EngineSpecificFromTokens(start, end))
	return body, withData
}

// trailingDataModifier finds the WITH [NO] DATA clause that may end a continuous
// aggregate after its body.
//
// Takes tokens ([]pgext.Token) which are the statement's tokens.
// Takes bodyStart (int) which is the index of the body's first token; a clause must start
// after it.
//
// Returns int which is the index of the clause's WITH, or len(tokens) when the statement
// has no such clause.
// Returns string which is literalFalse for WITH NO DATA and literalTrue otherwise.
func trailingDataModifier(tokens []pgext.Token, bodyStart int) (int, string) {
	count := len(tokens)
	if count < 2 || !tokens[count-1].IsKeyword(pgext.TokenIdentifier, "DATA") {
		return count, literalTrue
	}
	withIndex, withData := count-2, literalTrue
	if tokens[withIndex].IsKeyword(pgext.TokenIdentifier, "NO") {
		withIndex, withData = withIndex-1, literalFalse
	}
	if withIndex <= bodyStart || !tokens[withIndex].IsKeyword(pgext.TokenIdentifier, "WITH") {
		return count, literalTrue
	}
	return withIndex, withData
}
