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

// Policy and management calls produce a MutationAlterTableAlterColumn
// mutation tagged with TIMESCALE_POLICY_OP=<canonical-op>. The choice
// of mutation kind matches parser_hypertable.go's annotation form for
// SELECT create_hypertable: each call adjusts an existing relation's
// metadata rather than creating new schema state. Reusing
// MutationAlterTableAlterColumn keeps the catalogue dispatch table
// uniform (the alter handler already accepts EngineSpecific-only
// updates) and avoids introducing a new MutationKind that downstream
// consumers would have to handle in lockstep. The TIMESCALE_POLICY_OP
// tag carries the operation name so consumers can route on intent
// without re-classifying the kind themselves.

package timescaledb

import (
	"fmt"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

var (
	// policyOperationNames maps each policy/management statement kind to the canonical
	// TimescaleDB function name it represents. Used by parsePolicyCall to populate
	// TIMESCALE_POLICY_OP and by the per-kind dispatch tables so downstream consumers can
	// route on intent without re-classifying the kind themselves.
	policyOperationNames = map[pgext.StatementKind]string{
		statementKindAddCompressionPolicy:            "add_compression_policy",
		statementKindRemoveCompressionPolicy:         "remove_compression_policy",
		statementKindAddColumnstorePolicy:            "add_columnstore_policy",
		statementKindRemoveColumnstorePolicy:         "remove_columnstore_policy",
		statementKindAddRetentionPolicy:              "add_retention_policy",
		statementKindRemoveRetentionPolicy:           "remove_retention_policy",
		statementKindAddContinuousAggregatePolicy:    "add_continuous_aggregate_policy",
		statementKindRemoveContinuousAggregatePolicy: "remove_continuous_aggregate_policy",
		statementKindAddReorderPolicy:                "add_reorder_policy",
		statementKindRemoveReorderPolicy:             "remove_reorder_policy",
		statementKindAddJob:                          "add_job",
		statementKindAlterJob:                        "alter_job",
		statementKindDeleteJob:                       "delete_job",
		statementKindRunJob:                          "run_job",
		statementKindAddDimension:                    "add_dimension",
		statementKindSetChunkTimeInterval:            "set_chunk_time_interval",
		statementKindSetIntegerNowFunc:               "set_integer_now_func",
		statementKindEnableChunkSkipping:             "enable_chunk_skipping",
		statementKindDisableChunkSkipping:            "disable_chunk_skipping",
	}

	// jobOperationKinds names the kinds whose first positional argument is a job_id.
	//
	// These kinds take a job_id rather than a table reference. add_job is the registration
	// form which takes a procedure name first, while the others take an integer job_id.
	// Keeping the membership test as a small map avoids a long case list at the call site.
	jobOperationKinds = map[pgext.StatementKind]bool{
		statementKindAlterJob:  true,
		statementKindDeleteJob: true,
		statementKindRunJob:    true,
	}
)

// parsePolicyCall parses the shared shape of a SELECT-form policy or management call.
//
// The call shape is:
//
//	SELECT <function>(arg1 [, arg2] [, named => value] ...)
//
// The first positional argument is the target hypertable for most shapes, while the job
// operations (alter_job, delete_job, run_job) take a job_id instead, which is exposed as
// TIMESCALE_JOB_ID. Subsequent arguments are captured opaquely as TIMESCALE_POLICY_ARGS
// so downstream consumers can replay the original call without needing to re-tokenise.
//
// The mutation is MutationAlterTableAlterColumn so the catalogue does not refuse the call
// as a duplicate CREATE TABLE, since the targeted relation was created earlier in the
// migration. TIMESCALE_POLICY_OP carries the canonical operation name extracted from
// policyOperationNames.
//
// The work is split across small helpers. parsePolicyCall consumes SELECT, the operation
// keyword and the opening paren, dispatches to parsePolicyArguments for the body, then
// requires the end of the statement, so a call followed by FROM or WHERE, which may run
// it any number of times, is refused. parsePolicyArguments drives the first-argument
// extraction, applies it to the mutation, and either closes the call or delegates the
// tail capture. extractFirstPolicyArgument reads the single leading token (string,
// identifier, or number). applyFirstPolicyArgument routes the first argument into
// TIMESCALE_TABLE, TIMESCALE_JOB_ID or TIMESCALE_JOB_PROC.
// applyAddDimensionSecondArgument decodes the second argument for add_dimension calls
// (legacy column or by_range/by_hash). captureRemainingCallArguments gathers the opaque
// tail into TIMESCALE_POLICY_ARGS. This split keeps each step under the per-function size
// guideline and lets each helper test its own argument shape in isolation rather than
// re-exercising the entire chain.
//
// Takes p (pgext.ParserContext) which drives the parse.
// Takes kind (pgext.StatementKind) which selects the canonical operation name and
// disambiguates the first argument shape.
//
// Returns *ast.CatalogueMutation describing the call.
// Returns error on parse failure.
func parsePolicyCall(p pgext.ParserContext, kind pgext.StatementKind) (*ast.CatalogueMutation, error) {
	operation := policyOperationNames[kind]
	if operation == "" {
		return nil, dialectkit.NewParseError(p.CurrentToken(), fmt.Sprintf("timescaledb: policy kind %d has no registered operation name", kind))
	}

	if err := p.ExpectKeyword("SELECT"); err != nil {
		return nil, err
	}
	if !p.MatchKeyword(operation) {
		return nil, dialectkit.NewParseError(p.CurrentToken(), fmt.Sprintf("expected %s", operation))
	}
	if p.CurrentToken().Kind != pgext.TokenLeftParen {
		return nil, dialectkit.NewParseError(p.CurrentToken(), fmt.Sprintf("expected '(' after %s", operation))
	}
	openParen := p.Advance()

	mutation, parseErr := parsePolicyArguments(p, kind, operation, openParen)
	if parseErr != nil {
		return nil, parseErr
	}

	if err := expectCallEnd(p, operation); err != nil {
		return nil, err
	}
	return mutation, nil
}

// parsePolicyArguments captures the first positional argument and opaque tail of a
// policy/management call into the returned mutation. add_job has a procedure name first;
// the *_job operations take a job_id; add_dimension has the table reference followed by
// an optional dimension builder; everything else takes a hypertable reference.
//
// Takes p (pgext.ParserContext) which is positioned just inside the opening paren.
// Takes kind (pgext.StatementKind) which classifies the expected first-argument shape.
// Takes operation (string) which is the canonical function name used for error wrapping.
// Takes openParen (pgext.Token) which is the call's `(`, where an unterminated call is
// reported.
//
// Returns *ast.CatalogueMutation with EngineSpecific populated.
// Returns error on parse failure.
func parsePolicyArguments(
	p pgext.ParserContext,
	kind pgext.StatementKind,
	operation string,
	openParen pgext.Token,
) (*ast.CatalogueMutation, error) {
	mutation := ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, "", "", ast.WithEngineSpecific(map[string]string{
		"TIMESCALE_POLICY_OP": operation,
	}))

	firstArgument, firstErr := extractFirstPolicyArgument(p)
	if firstErr != nil {
		return nil, fmt.Errorf("%s: first argument: %w", operation, firstErr)
	}
	applyFirstPolicyArgument(mutation, kind, firstArgument)

	if !consumeArgumentSeparator(p) {
		if closeErr := expectCallClose(p, operation, openParen); closeErr != nil {
			return nil, closeErr
		}
		return mutation, nil
	}

	if kind == statementKindAddDimension {
		if dimensionErr := applyAddDimensionSecondArgument(p, mutation, operation); dimensionErr != nil {
			return nil, dimensionErr
		}
		if !consumeArgumentSeparator(p) {
			if closeErr := expectCallClose(p, operation, openParen); closeErr != nil {
				return nil, closeErr
			}
			return mutation, nil
		}
	}

	extras, extrasErr := captureRemainingCallArguments(p, operation, openParen)
	if extrasErr != nil {
		return nil, extrasErr
	}
	if extras != "" {
		mutation.EngineSpecific["TIMESCALE_POLICY_ARGS"] = extras
	}
	return mutation, nil
}

// applyAddDimensionSecondArgument decodes the second argument of an add_dimension call.
//
// The argument is either a literal column name (legacy positional form) or a
// "by_range(...)" or "by_hash(...)" builder. Both shapes set TIMESCALE_TIME_COLUMN, while
// the builder form additionally records TIMESCALE_DIMENSION_BUILDER so downstream
// consumers can distinguish range partitioning from hash partitioning.
//
// Takes p (pgext.ParserContext) which is positioned at the second argument.
// Takes mutation (*ast.CatalogueMutation) which receives the extracted keys.
// Takes operation (string) which is the canonical function name used for error wrapping.
//
// Returns error on a malformed second argument.
func applyAddDimensionSecondArgument(
	p pgext.ParserContext,
	mutation *ast.CatalogueMutation,
	operation string,
) error {
	dimension, dimensionErr := extractDimensionArgument(p)
	if dimensionErr != nil {
		return fmt.Errorf("%s: dimension argument: %w", operation, dimensionErr)
	}
	mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"] = dimension.column
	if dimension.builder != "" {
		mutation.EngineSpecific["TIMESCALE_DIMENSION_BUILDER"] = dimension.builder
	}
	return nil
}

// applyFirstPolicyArgument stores the first argument in the appropriate EngineSpecific
// key for the kind: TIMESCALE_JOB_ID for the bare-job operations, TIMESCALE_JOB_PROC for
// add_job, and TIMESCALE_TABLE otherwise. SchemaName and TableName are populated for
// non-job kinds so catalogue lookups continue to resolve the target relation.
//
// Takes mutation (*ast.CatalogueMutation) which receives the metadata.
// Takes kind (pgext.StatementKind) which selects the key.
// Takes arg (string) which is the captured first-argument text.
func applyFirstPolicyArgument(
	mutation *ast.CatalogueMutation,
	kind pgext.StatementKind,
	arg string,
) {
	if jobOperationKinds[kind] {
		mutation.EngineSpecific["TIMESCALE_JOB_ID"] = arg
		return
	}
	if kind == statementKindAddJob {
		mutation.EngineSpecific["TIMESCALE_JOB_PROC"] = arg
		return
	}
	mutation.EngineSpecific["TIMESCALE_TABLE"] = arg
	schema, table := splitMaybeSchemaQualified(arg)
	mutation.SchemaName = schema
	mutation.TableName = table
}

// extractFirstPolicyArgument reads a single string literal, bare identifier or numeric
// literal argument.
//
// The token is consumed and its text returned. Anything else (an open paren, an operator,
// or EOF) is rejected so a malformed call surfaces with a clear error.
//
// Only the single token at the cursor is captured, so a schema-qualified job procedure
// passed as a bare identifier triple ("schema . proc") is not assembled into a dotted
// name here. Callers that need schema-qualified routing rely on string-literal forms
// ("'schema.proc'") which the tokeniser emits as a single TokenString, and the downstream
// splitMaybeSchemaQualified call in applyFirstPolicyArgument then splits the dot for the
// hypertable branch. The job-proc branch stores the captured text verbatim in
// TIMESCALE_JOB_PROC and does not split, because TimescaleDB stores the procedure
// reference as a single name in its job-registry catalogue.
//
// Takes p (pgext.ParserContext) which is positioned at the candidate first token.
//
// Returns string which is the captured text.
// Returns error when the cursor token is not a recognised argument shape.
func extractFirstPolicyArgument(p pgext.ParserContext) (string, error) {
	current := p.CurrentToken()
	switch current.Kind {
	case pgext.TokenString, pgext.TokenIdentifier, pgext.TokenNumber:
		p.Advance()

		if castErr := consumeOptionalCast(p); castErr != nil {
			return "", castErr
		}
		return current.Value, nil
	default:
	}
	return "", dialectkit.NewParseError(current, "expected literal or identifier")
}
