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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

func TestNativeTypeAliasesAcrossDialects(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			for _, alias := range fuzzcase.TypeAliases(name) {
				t.Run(alias.Name, func(t *testing.T) {
					scenario := fuzzcase.AliasScenario(name, alias)
					query := expressionQuery(t, engine, strings.Join(scenario.Schema, ";\n"), scenario.Queries[0].SQL)
					require.Len(t, query.OutputColumns, 1)
					observed := query.OutputColumns[0].SQLType
					require.Equal(t, alias.Kind, observed.Category.String())
					require.Equal(t, alias.Width, observed.Width)
					require.Equal(t, alias.Unsigned, observed.Unsigned)
				})
			}
		})
	}
}
