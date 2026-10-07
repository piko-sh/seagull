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

package dialectkit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func TestLimitsSelectTheDefaultsAndHoldToTheCeilings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		limits     dialectkit.Limits
		wantDepth  int
		wantTokens int
	}{
		{name: "zero value", limits: dialectkit.Limits{MaxParseDepth: 0, MaxTokensPerStatement: 0},
			wantDepth: dialectkit.DefaultMaxParseDepth, wantTokens: dialectkit.DefaultMaxTokensPerStatement},
		{name: "negative values", limits: dialectkit.Limits{MaxParseDepth: -1, MaxTokensPerStatement: -1},
			wantDepth: dialectkit.DefaultMaxParseDepth, wantTokens: dialectkit.DefaultMaxTokensPerStatement},
		{name: "set values", limits: dialectkit.Limits{MaxParseDepth: 32, MaxTokensPerStatement: 500},
			wantDepth: 32, wantTokens: 500},
		{name: "values past the ceilings", limits: dialectkit.Limits{MaxParseDepth: 1 << 30, MaxTokensPerStatement: 1 << 40},
			wantDepth: dialectkit.MaxParseDepthCeiling, wantTokens: dialectkit.MaxTokensPerStatementCeiling},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.wantDepth, testCase.limits.ParseDepth())
			assert.Equal(t, testCase.wantTokens, testCase.limits.TokensPerStatement())
		})
	}
}

func TestLimitsWithMethodsIgnoreValuesBelowOneAndClampToTheCeilings(t *testing.T) {
	t.Parallel()

	start := dialectkit.Limits{MaxParseDepth: 10, MaxTokensPerStatement: 20}
	cases := []struct {
		name  string
		value int
		want  dialectkit.Limits
	}{
		{name: "zero", value: 0, want: start},
		{name: "negative", value: -7, want: start},
		{name: "one", value: 1, want: dialectkit.Limits{MaxParseDepth: 1, MaxTokensPerStatement: 1}},
		{name: "beyond both ceilings", value: 1 << 40,
			want: dialectkit.Limits{MaxParseDepth: dialectkit.MaxParseDepthCeiling, MaxTokensPerStatement: dialectkit.MaxTokensPerStatementCeiling}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := start.WithMaxParseDepth(testCase.value).WithMaxTokensPerStatement(testCase.value)

			assert.Equal(t, testCase.want, got)
			assert.Equal(t, dialectkit.Limits{MaxParseDepth: 10, MaxTokensPerStatement: 20}, start, "the receiver is not changed")
		})
	}
}

func TestLimitsCheckTokensAgainstTheBudgetInForce(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		limits  dialectkit.Limits
		count   int
		wantErr bool
	}{
		{name: "within a set budget", limits: dialectkit.Limits{MaxParseDepth: 0, MaxTokensPerStatement: 5}, count: 5, wantErr: false},
		{name: "over a set budget", limits: dialectkit.Limits{MaxParseDepth: 0, MaxTokensPerStatement: 5}, count: 6, wantErr: true},
		{name: "within the default", limits: dialectkit.Limits{MaxParseDepth: 0, MaxTokensPerStatement: 0},
			count: dialectkit.DefaultMaxTokensPerStatement, wantErr: false},
		{name: "over the default", limits: dialectkit.Limits{MaxParseDepth: 0, MaxTokensPerStatement: 0},
			count: dialectkit.DefaultMaxTokensPerStatement + 1, wantErr: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.limits.CheckTokens(testCase.count)

			if testCase.wantErr {
				assert.ErrorIs(t, err, dialectkit.ErrTokenBudget)
				return
			}
			assert.NoError(t, err)
		})
	}
}
