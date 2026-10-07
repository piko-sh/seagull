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

package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestCatalogueHoldsUnknownTypePassesOverNilCatalogueEntries(t *testing.T) {
	t.Parallel()

	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "citext")
	tests := []struct {
		name    string
		schemas map[string]*sqlmodel.Schema
		want    bool
	}{
		{name: "a nil schema", schemas: map[string]*sqlmodel.Schema{"main": nil}, want: false},
		{name: "a nil table", schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"t": nil}}}, want: false},
		{name: "a nil view", schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Views: map[string]*sqlmodel.View{"v": nil}}}, want: false},
		{name: "a nil table beside a view holding the type", schemas: map[string]*sqlmodel.Schema{"main": {
			Name:   "main",
			Tables: map[string]*sqlmodel.Table{"t": nil},
			Views:  map[string]*sqlmodel.View{"v": {Name: "v", Columns: []sqlmodel.Column{sqlmodel.NewColumn("name", unknown, true)}}},
		}}, want: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var holds bool
			require.NotPanics(t, func() { holds = catalogueHoldsUnknownType(&sqlmodel.Catalogue{Schemas: testCase.schemas}, "citext") })
			assert.Equal(t, testCase.want, holds)
		})
	}
}
