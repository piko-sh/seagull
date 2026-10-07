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

package check

import (
	"context"
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// asyncExecNotSupportedMessage closes the Q041 diagnostic for a query that declares
// asyncexec on an engine whose dialect reports Info.AsyncMutations as false.
const asyncExecNotSupportedMessage = "asyncexec is only supported on engines that surface asynchronous mutation semantics"

// asyncExecPass validates pairings of the QueryCommandAsyncExec command with the engine's
// capability flag and recommends asyncexec over exec when the underlying statement is an
// asynchronous mutation.
//
// The recommendation applies only to exec queries; row-producing commands are excluded.
//
// Two diagnostic paths exist. Q041 is a hard error when the query declares asyncexec but
// the engine reports Info().AsyncMutations == false; this prevents a silent downgrade to
// synchronous Exec on engines that lack the distinction between mutation acceptance and
// completion. Q040 is a hint when the query declares exec on an engine that supports
// async mutations and the dialect marked the statement as an asynchronous mutation; the
// user is nudged to declare asyncexec so the generated method reflects the
// fire-and-forget semantics.
type asyncExecPass struct {
	// dialect holds what the pass reads of the dialect, its name and asynchronous-mutation
	// capability; nil when there is no dialect, and both diagnostic paths then skip.
	dialect *dialectFacts
}

// Analyse inspects the query command against the engine's asynchronous-mutation
// capability and whether the dialect marked the statement as an asynchronous mutation.
//
// Takes subject (*DiagnosticContext) which holds the query and its raw analysis.
//
// Returns []diag.SourceError which holds at most one diagnostic. The hard-error path
// (Q041) takes precedence over the hint-level recommendation.
func (p *asyncExecPass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	if p.dialect == nil {
		return nil
	}

	engineSupports := p.dialect.info.AsyncMutations

	if subject.Query.Command == sqlmodel.QueryCommandAsyncExec && !engineSupports {
		return []diag.SourceError{
			diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeAsyncExecNotSupported, diag.SeverityError, fmt.Sprintf(
				"query %q declares asyncexec but dialect %q does not surface asynchronous mutation semantics: %s",
				subject.Query.Name, p.dialect.info.Name, asyncExecNotSupportedMessage,
			)),
		}
	}

	if subject.Query.Command == sqlmodel.QueryCommandExec && engineSupports && isAsyncMutation(subject.RawAnalysis) {
		return []diag.SourceError{
			diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeAsyncExecRecommended, diag.SeverityHint, fmt.Sprintf(
				"query %q uses 'exec' with an asynchronous mutation; consider 'asyncexec' to surface the fire-and-forget semantics",
				subject.Query.Name,
			)),
		}
	}

	return nil
}

// isAsyncMutation reports whether the dialect marked the statement as a mutation the
// server completes asynchronously.
//
// Takes analysis (*ast.RawQueryAnalysis) which may be nil.
//
// Returns bool which is true when the statement is an asynchronous mutation.
func isAsyncMutation(analysis *ast.RawQueryAnalysis) bool {
	return analysis != nil && analysis.AsyncMutation
}
