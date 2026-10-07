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

package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallSpecResolvesItsArguments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		lookup         string
		wantPositional int
		wantKeyword    bool
	}{
		{name: "the first positional", lookup: "name", wantPositional: 0, wantKeyword: false},
		{name: "the second positional", lookup: "command", wantPositional: 1, wantKeyword: false},
		{name: "a keyword argument", lookup: "readonly", wantPositional: -1, wantKeyword: true},
		{name: "an unknown name", lookup: "nope", wantPositional: -1, wantKeyword: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			positional, index, isPositional := testQuerySpec.Positional(testCase.lookup)
			keywordArgument, isKeyword := testQuerySpec.KeywordArgument(testCase.lookup)

			assert.Equal(t, testCase.wantKeyword, isKeyword)
			if testCase.wantKeyword {
				require.NotNil(t, keywordArgument)
				assert.Equal(t, testCase.lookup, keywordArgument.Name)
			}
			if testCase.wantPositional < 0 {
				assert.False(t, isPositional)
				assert.Nil(t, positional)
				return
			}
			require.True(t, isPositional)
			assert.Equal(t, testCase.wantPositional, index)
			assert.Equal(t, testCase.lookup, positional.Name)
		})
	}
}

func TestCallSpecArgumentNamesListsPositionalsThenKeywords(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"name", "command", "readonly", "max"}, testQuerySpec.ArgumentNames())
	assert.Empty(t, (&CallSpec{Positionals: nil, KeywordArguments: nil}).ArgumentNames())
}

func TestMigrationAndColumnCallSpecsDeclareTheirArguments(t *testing.T) {
	t.Parallel()

	migration := MigrationCallSpec()
	column := ColumnCallSpec()

	assert.Equal(t, []string{"readonly", "no_transaction"}, migration.ArgumentNames())
	assert.Equal(t, []string{"name", "type", "go_type", "nullable", "non_null_elements", "struct_tags"}, column.ArgumentNames())
	require.Len(t, column.Positionals, 1)
	assert.True(t, column.Positionals[0].Required)
	assert.Equal(t, KeywordArgumentQualifiedIdentifier, column.Positionals[0].Kind)
	for _, key := range []string{"readonly", "no_transaction"} {
		argument, found := migration.KeywordArgument(key)
		require.True(t, found, key)
		assert.Equal(t, KeywordArgumentBool, argument.Kind, key)
	}
}
