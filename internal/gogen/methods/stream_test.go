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

	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func streamQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:          "StreamEvents",
		Filename:      "events.sql",
		Command:       sqlmodel.QueryCommandStream,
		ReadOnly:      true,
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
		Template:      template(text("SELECT id FROM events")),
	}
}

func TestStreamMethodReturnsTheIteratorTypeTheStyleAsksFor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		plan         map[string]string
		name         string
		want         string
		wantImport   string
		modernIdioms bool
	}{
		{name: "current idioms", modernIdioms: true, want: "iter.Seq2[StreamEventsRow, error]", wantImport: "iter"},
		{name: "current idioms with iter planned under another name", modernIdioms: true, plan: map[string]string{"iter": "iterators"},
			want: "iterators.Seq2[StreamEventsRow, error]", wantImport: "iterators"},
		{name: "the spelt-out type", modernIdioms: false, want: "func(func(StreamEventsRow, error) bool)"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := runtimeEmission()
			emission.Request.Style.ModernIdioms = testCase.modernIdioms
			query := queryFor(t, emission, streamQuery())
			query.Imports = goast.NewPlannedImportTracker(testCase.plan)

			method := buildStreamMethod(emission, query, StaticPrelude(query))

			source := renderDecl(t, method)
			require.Contains(t, source, "func (queries *Queries) StreamEvents(ctx context.Context) "+testCase.want+" {")
			require.Contains(t, source, "return func(yield func(StreamEventsRow, error) bool) {")
			if testCase.wantImport == "" {
				require.Empty(t, query.Imports.Qualifiers())
				return
			}
			require.Equal(t, map[string]string{testCase.wantImport: "iter"}, query.Imports.Qualifiers())
		})
	}
}
