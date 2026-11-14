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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestWholeClauseProblems(t *testing.T) {
	t.Parallel()

	ordered := "SELECT p.id FROM quotes p WHERE p.deleted_at IS NULL ORDER BY date(p.quote_date) DESC, p.id DESC LIMIT $1 OFFSET $2"
	orderedSpans := func(change func(spans *ast.StatementSpans)) ast.StatementSpans {
		spans := ast.StatementSpans{
			Projection:     spanOf(ordered, "p.id"),
			From:           spanOf(ordered, "FROM quotes p"),
			Where:          spanOf(ordered, "WHERE p.deleted_at IS NULL"),
			WhereCondition: spanOf(ordered, "p.deleted_at IS NULL"),
			OrderBy:        spanOf(ordered, "ORDER BY date(p.quote_date) DESC, p.id DESC"),
			Pagination:     spanOf(ordered, "LIMIT $1 OFFSET $2"),
		}
		change(&spans)
		return spans
	}
	nested := "SELECT p.id FROM quotes p WHERE p.id IN (SELECT q.id FROM quotes q) ORDER BY p.id"
	nestedSpans := func(change func(spans *ast.StatementSpans)) ast.StatementSpans {
		spans := ast.StatementSpans{
			Projection:     spanOf(nested, "p.id"),
			From:           spanOf(nested, "FROM quotes p"),
			Where:          spanOf(nested, "WHERE p.id IN (SELECT q.id FROM quotes q)"),
			WhereCondition: spanOf(nested, "p.id IN (SELECT q.id FROM quotes q)"),
			OrderBy:        spanOf(nested, "ORDER BY p.id"),
		}
		change(&spans)
		return spans
	}
	parenthesised := "(SELECT p.id FROM quotes p ORDER BY p.id)"
	parenthesisedSpans := func(change func(spans *ast.StatementSpans)) ast.StatementSpans {
		spans := ast.StatementSpans{
			Projection: spanOf(parenthesised, "p.id"),
			From:       spanOf(parenthesised, "FROM quotes p"),
			OrderBy:    spanOf(parenthesised, "ORDER BY p.id"),
		}
		change(&spans)
		return spans
	}
	unchanged := func(*ast.StatementSpans) {}
	testCases := []struct {
		name     string
		text     string
		problems []string
		spans    ast.StatementSpans
	}{
		{
			name:     "every clause covered whole",
			text:     ordered,
			spans:    orderedSpans(unchanged),
			problems: []string{},
		},
		{
			name:     "a WHERE that holds a whole nested query",
			text:     nested,
			spans:    nestedSpans(unchanged),
			problems: []string{},
		},
		{
			name:     "a clause a closing parenthesis follows",
			text:     parenthesised,
			spans:    parenthesisedSpans(unchanged),
			problems: []string{},
		},
		{
			name:  "an ORDER BY that stops inside its first key",
			text:  ordered,
			spans: orderedSpans(func(spans *ast.StatementSpans) { spans.OrderBy = spanOf(ordered, "ORDER BY date") }),
			problems: []string{
				`OrderBy "ORDER BY date" ends before "(", inside its clause`,
			},
		},
		{
			name:  "an ORDER BY that stops before a closing parenthesis it needs",
			text:  ordered,
			spans: orderedSpans(func(spans *ast.StatementSpans) { spans.OrderBy = spanOf(ordered, "ORDER BY date(p.quote_date") }),
			problems: []string{
				`OrderBy "ORDER BY date(p.quote_date" ends before ")", inside its clause`,
			},
		},
		{
			name:  "an ORDER BY that stops inside a token",
			text:  ordered,
			spans: orderedSpans(func(spans *ast.StatementSpans) { spans.OrderBy = spanOf(ordered, "ORDER BY date(p.quote_d") }),
			problems: []string{
				`OrderBy "ORDER BY date(p.quote_d" ends inside "quote_date"`,
			},
		},
		{
			name: "an ORDER BY that starts inside a token",
			text: ordered,
			spans: orderedSpans(func(spans *ast.StatementSpans) {
				spans.OrderBy = spanOf(ordered, "RDER BY date(p.quote_date) DESC, p.id DESC")
			}),
			problems: []string{
				`OrderBy "RDER BY date(p.quote_date) DESC, p.id DESC" starts inside "ORDER"`,
			},
		},
		{
			name:  "an ORDER BY that starts after its keyword",
			text:  ordered,
			spans: orderedSpans(func(spans *ast.StatementSpans) { spans.OrderBy = spanOf(ordered, "date(p.quote_date) DESC, p.id DESC") }),
			problems: []string{
				`OrderBy "date(p.quote_date) DESC, p.id DESC" starts at "date", not at the keyword that opens it`,
			},
		},
		{
			name:  "a pagination that leaves its OFFSET out",
			text:  ordered,
			spans: orderedSpans(func(spans *ast.StatementSpans) { spans.Pagination = spanOf(ordered, "LIMIT $1") }),
			problems: []string{
				`Pagination "LIMIT $1" ends before "OFFSET", inside its clause`,
			},
		},
		{
			name:  "a WHERE condition that stops inside the condition",
			text:  ordered,
			spans: orderedSpans(func(spans *ast.StatementSpans) { spans.WhereCondition = spanOf(ordered, "p.deleted_at") }),
			problems: []string{
				`WhereCondition "p.deleted_at" ends before "IS", inside its clause`,
			},
		},
		{
			name:  "a FROM that stops before its alias",
			text:  ordered,
			spans: orderedSpans(func(spans *ast.StatementSpans) { spans.From = spanOf(ordered, "FROM quotes") }),
			problems: []string{
				`From "FROM quotes" ends before "p", inside its clause`,
			},
		},
		{
			name:  "a WHERE that stops before the nested query it opens",
			text:  nested,
			spans: nestedSpans(func(spans *ast.StatementSpans) { spans.Where = spanOf(nested, "WHERE p.id IN (") }),
			problems: []string{
				`Where "WHERE p.id IN (" ends before "SELECT", inside its clause`,
			},
		},
		{
			name: "a select list that covers only the space after SELECT",
			text: ordered,
			spans: orderedSpans(func(spans *ast.StatementSpans) {
				spans.Projection = ast.Span{Start: len("SELECT"), End: len("SELECT ")}
			}),
			problems: []string{
				`Projection {Start:6 End:7} covers no token`,
			},
		},
		{
			name:  "an ORDER BY that takes in the parenthesis closing the statement",
			text:  parenthesised,
			spans: parenthesisedSpans(func(spans *ast.StatementSpans) { spans.OrderBy = spanOf(parenthesised, "ORDER BY p.id)") }),
			problems: []string{
				`OrderBy "ORDER BY p.id)" does not balance its parentheses`,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			subject := spanTestSubject(t, testCase.text, &ast.RawQueryAnalysis{Spans: testCase.spans})

			require.Equal(t, testCase.problems, wholeClauseProblems(spanTestCorpus(), subject))
		})
	}
}
