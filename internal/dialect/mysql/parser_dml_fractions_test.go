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

package mysql

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestConstantFractionsFollowNativeIntegerRounding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		text     string
		category string
		want     string
	}{
		{text: "-8.5", category: "decimal", want: "-9"}, {text: "-8.5e0", category: "double", want: "-8"},
		{text: "-0.5", category: "decimal", want: "-1"}, {text: "-0.5e0", category: "double", want: "0"},
		{text: "0.5", category: "decimal", want: "1"}, {text: "0.5e0", category: "double", want: "0"},
		{text: "1.5e0", category: "double", want: "2"}, {text: "2.5e0", category: "double", want: "2"},
		{text: "-0.49", category: "decimal", want: "0"}, {text: "-0.51", category: "decimal", want: "-1"},
	} {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()
			p := newParser(nil)
			p.constantNumbers = make(map[ast.Expression]*big.Int)
			literal := &ast.LiteralExpression{TypeName: test.category}
			p.rememberIntegerLiteral(literal, test.text)
			value, known := p.fractionNumber(literal, 0)
			require.True(t, known)
			require.Equal(t, test.want, value.integer().String())
			require.Nil(t, p.constantNumber(literal, 0), "fractional values require an integer cast")
		})
	}
}

func TestConstantFractionsBoundEvaluation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ text, category string }{
		{text: "1e1000000", category: "double"}, {text: "NaN", category: "double"}, {text: "Inf", category: "double"},
		{text: "1e160", category: "double"}, {text: "0." + strings.Repeat("0", 200) + "1", category: "decimal"},
	} {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()
			p := newParser(nil)
			p.constantNumbers = make(map[ast.Expression]*big.Int)
			literal := &ast.LiteralExpression{TypeName: test.category}
			p.rememberIntegerLiteral(literal, test.text)
			_, known := p.fractionNumber(literal, 0)
			require.False(t, known)
		})
	}
	p := newParser(nil)
	p.constantNumbers = make(map[ast.Expression]*big.Int)
	literal := &ast.LiteralExpression{TypeName: "decimal"}
	p.rememberIntegerLiteral(literal, "1.5")
	_, known := p.fractionNumber(literal, constantExpressionDepth)
	require.False(t, known)
	_, known = p.fractionNumber(&ast.ParameterExpression{}, 0)
	require.False(t, known)
	large := constantFraction{value: new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 300)), approximate: false}
	_, known = fractionBinary("*", large, large)
	require.False(t, known, "intermediate values must remain bounded")
	nonfinite := constantFraction{value: new(big.Rat).SetFloat64(math.MaxFloat64), approximate: true}
	_, known = fractionBinary("*", nonfinite, nonfinite)
	require.False(t, known, "floating point overflow must remain unknown")
}
