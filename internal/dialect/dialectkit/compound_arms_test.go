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

package dialectkit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestSplitCompoundArmsGivesEachArmWhatItGathered(t *testing.T) {
	t.Parallel()

	parameter := func(number int) ast.RawParameterReference { return ast.RawParameterReference{Number: number} }
	derived := func(alias string) ast.RawDerivedTableReference { return ast.RawDerivedTableReference{Alias: alias} }
	function := func(alias string) ast.RawTableValuedFunctionReference {
		return ast.RawTableValuedFunctionReference{FunctionName: "series", Alias: alias}
	}
	predicate := &ast.RawQueryAnalysis{}

	var gathered Gathered
	gathered.Parameters = append(gathered.Parameters, parameter(1))
	gathered.DerivedTables = append(gathered.DerivedTables, derived("first"))
	firstArmStart := gathered.Mark()
	gathered.Parameters = append(gathered.Parameters, parameter(2))
	gathered.DerivedTables = append(gathered.DerivedTables, derived("second"))
	gathered.TableFunctions = append(gathered.TableFunctions, function("numbers"))
	firstArmEnd := gathered.Mark()
	secondArmStart := gathered.Mark()
	gathered.PredicateSubqueries = append(gathered.PredicateSubqueries, predicate)
	secondArmEnd := gathered.Mark()
	gathered.Parameters = append(gathered.Parameters, parameter(3))

	first, second := &ast.RawQueryAnalysis{}, &ast.RawQueryAnalysis{}
	query := &ast.RawQueryAnalysis{ReadOnly: true, ArgumentNames: map[int]string{2: "limit"}, CompoundBranches: []ast.RawCompoundBranch{
		{Operator: ast.CompoundUnion, Query: first},
		{Operator: ast.CompoundUnion, Query: second},
	}}

	require.NoError(t, SplitCompoundArms(query, gathered, []ArmExtent{{Start: firstArmStart, End: firstArmEnd}, {Start: secondArmStart, End: secondArmEnd}}))

	require.Equal(t, gathered.Parameters, query.ParameterReferences)
	require.Equal(t, []ast.RawDerivedTableReference{derived("first")}, query.RawDerivedTables)
	require.Empty(t, query.RawTableValuedFunctions)
	require.Empty(t, query.PredicateSubqueries)

	require.Equal(t, []ast.RawParameterReference{parameter(2)}, first.ParameterReferences)
	require.Equal(t, []ast.RawDerivedTableReference{derived("second")}, first.RawDerivedTables)
	require.Equal(t, []ast.RawTableValuedFunctionReference{function("numbers")}, first.RawTableValuedFunctions)
	require.Empty(t, first.PredicateSubqueries)
	require.True(t, first.ReadOnly)
	require.Equal(t, query.ArgumentNames, first.ArgumentNames)

	require.Empty(t, second.ParameterReferences)
	require.Empty(t, second.RawDerivedTables)
	require.Equal(t, []*ast.RawQueryAnalysis{predicate}, second.PredicateSubqueries)
}

func TestSplitCompoundArmsRefusesExtentsThatDoNotFitWhatWasGathered(t *testing.T) {
	t.Parallel()

	parameters := func(count int) []ast.RawParameterReference {
		return make([]ast.RawParameterReference, count)
	}
	mark := func(parameters int) ArmMark {
		return ArmMark{parameters: parameters, derivedTables: 0, tableFunctions: 0, predicateSubqueries: 0}
	}
	cases := []struct {
		name    string
		extents []ArmExtent
		wantErr bool
	}{
		{name: "arms in order within the list", extents: []ArmExtent{{Start: mark(1), End: mark(2)}, {Start: mark(2), End: mark(3)}}, wantErr: false},
		{name: "an arm that ends past the list", extents: []ArmExtent{{Start: mark(1), End: mark(4)}}, wantErr: true},
		{name: "an arm that ends before it starts", extents: []ArmExtent{{Start: mark(2), End: mark(1)}}, wantErr: true},
		{name: "an arm that starts past the list", extents: []ArmExtent{{Start: mark(5), End: mark(5)}}, wantErr: true},
		{name: "arms out of order", extents: []ArmExtent{{Start: mark(2), End: mark(3)}, {Start: mark(1), End: mark(2)}}, wantErr: true},
		{name: "arms that overlap", extents: []ArmExtent{{Start: mark(0), End: mark(2)}, {Start: mark(1), End: mark(3)}}, wantErr: true},
		{name: "no arms", extents: nil, wantErr: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gathered := Gathered{Parameters: parameters(3), DerivedTables: nil, TableFunctions: nil, PredicateSubqueries: nil}
			arms := make([]ast.RawCompoundBranch, len(testCase.extents))
			for index := range arms {
				arms[index] = ast.RawCompoundBranch{Operator: ast.CompoundUnion, Query: &ast.RawQueryAnalysis{}}
			}
			query := &ast.RawQueryAnalysis{CompoundBranches: arms}

			err := SplitCompoundArms(query, gathered, testCase.extents)

			if !testCase.wantErr {
				require.NoError(t, err)
				require.Len(t, query.ParameterReferences, 3)
				return
			}
			require.ErrorIs(t, err, ErrArmExtent)
			require.Nil(t, query.ParameterReferences, "a refused split leaves the query unchanged")
		})
	}
}
