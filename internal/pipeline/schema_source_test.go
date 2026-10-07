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

func TestDropNilEntriesLeavesOnlyRealEntries(t *testing.T) {
	t.Parallel()

	table := &sqlmodel.Table{Name: "items"}
	view := &sqlmodel.View{Name: "item_view"}
	function := &sqlmodel.Function{Name: "lower"}
	tests := []struct {
		name      string
		catalogue *sqlmodel.Catalogue
		want      *sqlmodel.Catalogue
	}{
		{name: "no catalogue", catalogue: nil, want: nil},
		{
			name:      "a nil schema is dropped",
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{"main": nil, "public": {Name: "public"}}},
			want:      &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{"public": {Name: "public"}}},
		},
		{
			name: "nil tables, views and other entries are dropped",
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{"main": {
				Name:           "main",
				Tables:         map[string]*sqlmodel.Table{"items": table, "gone": nil},
				Views:          map[string]*sqlmodel.View{"item_view": view, "gone": nil},
				Enums:          map[string]*sqlmodel.Enum{"gone": nil},
				CompositeTypes: map[string]*sqlmodel.CompositeType{"gone": nil},
				Sequences:      map[string]*sqlmodel.Sequence{"gone": nil},
				Domains:        map[string]*sqlmodel.Domain{"gone": nil},
				Functions:      map[string][]*sqlmodel.Function{"lower": {nil, function, nil}},
			}}},
			want: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{"main": {
				Name:           "main",
				Tables:         map[string]*sqlmodel.Table{"items": table},
				Views:          map[string]*sqlmodel.View{"item_view": view},
				Enums:          map[string]*sqlmodel.Enum{},
				CompositeTypes: map[string]*sqlmodel.CompositeType{},
				Sequences:      map[string]*sqlmodel.Sequence{},
				Domains:        map[string]*sqlmodel.Domain{},
				Functions:      map[string][]*sqlmodel.Function{"lower": {function}},
			}}},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NotPanics(t, func() { dropNilEntries(testCase.catalogue) })
			assert.Equal(t, testCase.want, testCase.catalogue)
		})
	}
}
