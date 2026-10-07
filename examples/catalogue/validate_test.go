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

package catalogue

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckStructureChecksEachConcept(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		concepts []Concept
		want     []string
	}{
		{
			name: "a well-formed concept passes",
			concepts: []Concept{
				{Number: 1, Slug: "first-query", Title: "Your first query", Summary: "One query.", Track: "start"},
			},
			want: nil,
		},
		{
			name: "numbers are contiguous",
			concepts: []Concept{
				{Number: 2, Slug: "first-query", Title: "Your first query", Summary: "One query.", Track: "start"},
			},
			want: []string{"concept 2 first-query: numbered 2, want 1"},
		},
		{
			name: "slugs are unique",
			concepts: []Concept{
				{Number: 1, Slug: "first-query", Title: "Your first query", Summary: "One query.", Track: "start"},
				{Number: 2, Slug: "first-query", Title: "Again", Summary: "Again.", Track: "start"},
			},
			want: []string{"concept 2 first-query: duplicate slug"},
		},
		{
			name: "a slug may not name an engine",
			concepts: []Concept{
				{Number: 1, Slug: "sqlite", Title: "SQLite", Summary: "The engine.", Track: "start"},
			},
			want: []string{"concept 1 sqlite: slug equals an engine name"},
		},
		{
			name: "the track must exist",
			concepts: []Concept{
				{Number: 1, Slug: "first-query", Title: "Your first query", Summary: "One query.", Track: "nowhere"},
			},
			want: []string{`concept 1 first-query: unknown track "nowhere"`},
		},
		{
			name: "a title and a summary are required",
			concepts: []Concept{
				{Number: 1, Slug: "first-query", Track: "start"},
			},
			want: []string{"concept 1 first-query: needs a title and a summary"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			curriculum := &Curriculum{
				Version:  1,
				Engines:  map[string]Engine{"sqlite": {Title: "SQLite", Dialect: "sqlite", Module: "sqlite"}},
				Tracks:   []Track{{ID: "start"}},
				Concepts: test.concepts,
			}
			problems := curriculum.checkStructure()
			require.Len(t, problems, len(test.want))
			for index, want := range test.want {
				require.ErrorContains(t, problems[index], want)
			}
		})
	}
}
