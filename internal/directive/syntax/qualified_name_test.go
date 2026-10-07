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
)

func TestSplitQualifiedNameKeepsQuotedPartsAndTheirCase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		input  string
		want   []NamePart
		wantOK bool
	}{
		{
			name:   "an unquoted table and column keep their case",
			input:  "events.UserID",
			want:   []NamePart{{Name: "events", Written: "events", Quoted: false}, {Name: "UserID", Written: "UserID", Quoted: false}},
			wantOK: true,
		},
		{
			name:   "a quoted column",
			input:  `events."UserID"`,
			want:   []NamePart{{Name: "events", Written: "events", Quoted: false}, {Name: "UserID", Written: `"UserID"`, Quoted: true}},
			wantOK: true,
		},
		{
			name:  "a quoted schema holding a dot and a doubled quote",
			input: `"my.schema"."Ta""ble".id`,
			want: []NamePart{
				{Name: "my.schema", Written: `"my.schema"`, Quoted: true},
				{Name: `Ta"ble`, Written: `"Ta""ble"`, Quoted: true},
				{Name: "id", Written: "id", Quoted: false},
			},
			wantOK: true,
		},
		{name: "a single name", input: "id", want: []NamePart{{Name: "id", Written: "id", Quoted: false}}, wantOK: true},
		{name: "an empty part", input: "events..id", want: nil, wantOK: false},
		{name: "a trailing dot", input: "events.", want: nil, wantOK: false},
		{name: "a quote left open", input: `events."UserID`, want: nil, wantOK: false},
		{name: "text after a quoted part", input: `events."User"ID`, want: nil, wantOK: false},
		{name: "an empty quoted part", input: `events.""`, want: nil, wantOK: false},
		{name: "more parts than a column name has", input: "a.b.c.d", want: nil, wantOK: false},
		{name: "nothing at all", input: "", want: nil, wantOK: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parts, ok := SplitQualifiedName(testCase.input)

			assert.Equal(t, testCase.wantOK, ok)
			assert.Equal(t, testCase.want, parts)
		})
	}
}
