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

package methods

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen/goast"
)

func TestPlaceholderHelpersFollowTheDialect(t *testing.T) {
	t.Parallel()

	limited := testDialectInfo(dollar, dialect.PlaceholderSigil{})
	unlimited := testDialectInfo(dollar, dialect.PlaceholderSigil{})
	unlimited.MaxParameters = 0
	backtick := testDialectInfo(dollar, dialect.PlaceholderSigil{})
	backtick.IdentifierQuote = dialect.IdentifierQuote{Open: "`", Close: "`"}

	testCases := []struct {
		name   string
		quoted string
		info   dialect.Facts
		limit  int
	}{
		{name: "a dialect with a limit", info: limited, quoted: `"order"`, limit: 999},
		{name: "a dialect with no limit", info: unlimited, quoted: `"order"`, limit: goast.DefaultMaxBindVariablesFallback},
		{name: "a dialect quoting with backticks", info: backtick, quoted: "`order`", limit: 999},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := testEmission(testCase.info, textMappings(), testDriver())

			require.Equal(t, testCase.quoted, QuoteIdentifier(emission, "order"))
			require.Equal(t, testCase.limit, MaxBindVariables(emission))
		})
	}
}
