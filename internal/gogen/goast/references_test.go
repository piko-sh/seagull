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

package goast

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectorQualifiersYieldsTheLeftOfEverySelectorInSourceOrder(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a file without selectors yields nothing",
			source: "package store\n\nfunc run() int { return 1 }\n",
			want:   nil,
		},
		{
			name:   "each selector yields its qualifier once, repeats included",
			source: "package store\n\nimport (\n\t\"database/sql\"\n\t\"time\"\n)\n\nvar (\n\t_ = sql.ErrNoRows\n\t_ = time.Second\n\t_ = sql.ErrTxDone\n)\n",
			want:   []string{"sql", "time", "sql"},
		},
		{
			name:   "a field selector on a call yields only the identifier on the left",
			source: "package store\n\nfunc run(row value) any { return row.next().field }\n",
			want:   []string{"row"},
		},
		{
			name:   "a selector nested in a signature yields its qualifier",
			source: "package store\n\nfunc run(ctx context.Context, values []json.RawMessage) error { return nil }\n",
			want:   []string{"context", "json"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile(token.NewFileSet(), "store.go", testCase.source, parser.SkipObjectResolution)
			require.NoError(t, err)
			require.Equal(t, testCase.want, slices.Collect(SelectorQualifiers(file)))
		})
	}
}

func TestSelectorQualifiersStopsWhenTheConsumerStops(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		node ast.Node
		name string
		want []string
		take int
	}{
		{
			name: "taking one yields only the first",
			node: &ast.BlockStmt{List: []ast.Stmt{
				&ast.ExprStmt{X: &ast.SelectorExpr{X: ast.NewIdent("first"), Sel: ast.NewIdent("Run")}},
				&ast.ExprStmt{X: &ast.SelectorExpr{X: ast.NewIdent("second"), Sel: ast.NewIdent("Run")}},
			}},
			take: 1,
			want: []string{"first"},
		},
		{
			name: "taking none yields nothing",
			node: &ast.SelectorExpr{X: ast.NewIdent("only"), Sel: ast.NewIdent("Run")},
			take: 0,
			want: []string{},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			taken := []string{}
			for qualifier := range SelectorQualifiers(testCase.node) {
				if len(taken) == testCase.take {
					break
				}
				taken = append(taken, qualifier)
			}
			require.Equal(t, testCase.want, taken)
		})
	}
}
