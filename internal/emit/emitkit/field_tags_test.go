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

package emitkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/testkit/scaling"
)

var fieldTagCases = []struct {
	style      emit.Style
	columnTags []map[string]string
	name       string
	names      []string
	want       []string
}{
	{
		name:  "the default writes the column's name as it is written",
		names: []string{"id", "createdAt", "user_name"},
		want:  []string{`json:"id"`, `json:"createdAt"`, `json:"user_name"`},
	},
	{
		name:  "a repeated name is made unique and an unsafe one safe",
		names: []string{"id", "id", "a\"b"},
		want:  []string{`json:"id"`, `json:"id2"`, `json:"a_b"`},
	},
	{
		name:  "snake case",
		style: emit.Style{JSONTags: emit.JSONTags{Case: emit.JSONCaseSnake}},
		names: []string{"createdAt", "userID", "HTTPStatus", "item_id", "Address2"},
		want:  []string{`json:"created_at"`, `json:"user_id"`, `json:"http_status"`, `json:"item_id"`, `json:"address2"`},
	},
	{
		name:  "camel case",
		style: emit.Style{JSONTags: emit.JSONTags{Case: emit.JSONCaseCamel}},
		names: []string{"item_id", "id", "createdAt", "USER_NAME", "full name"},
		want:  []string{`json:"itemId"`, `json:"id"`, `json:"createdAt"`, `json:"userName"`, `json:"fullName"`},
	},
	{
		name:  "pascal case",
		style: emit.Style{JSONTags: emit.JSONTags{Case: emit.JSONCasePascal}},
		names: []string{"item_id", "id"},
		want:  []string{`json:"ItemId"`, `json:"Id"`},
	},
	{
		name:  "names that meet once cased are made unique",
		style: emit.Style{JSONTags: emit.JSONTags{Case: emit.JSONCaseSnake}},
		names: []string{"user_name", "userName"},
		want:  []string{`json:"user_name"`, `json:"user_name2"`},
	},
	{
		name:  "omitempty",
		style: emit.Style{JSONTags: emit.JSONTags{OmitEmpty: true}},
		names: []string{"id"},
		want:  []string{`json:"id,omitempty"`},
	},
	{
		name:  "no json tags",
		style: emit.Style{JSONTags: emit.JSONTags{Omit: true, OmitEmpty: true}},
		names: []string{"id", "title"},
		want:  []string{"", ""},
	},
	{
		name:  "project-wide tags follow json, by key, with the name placeholder",
		style: emit.Style{StructTags: map[string]string{"yaml": "{name},omitempty", "db": "{name}"}, JSONTags: emit.JSONTags{Case: emit.JSONCaseCamel}},
		names: []string{"item_id", "item_id"},
		want:  []string{`json:"itemId" db:"item_id" yaml:"item_id,omitempty"`, `json:"itemId2" db:"item_id2" yaml:"item_id2,omitempty"`},
	},
	{
		name:       "a column's own tags take precedence key by key, json too",
		style:      emit.Style{StructTags: map[string]string{"db": "{name}"}},
		names:      []string{"email", "password_hash", "id"},
		columnTags: []map[string]string{{"validate": "required,email", "db": "mail"}, {"json": "-"}, nil},
		want:       []string{`json:"email" db:"mail" validate:"required,email"`, `json:"-" db:"password_hash"`, `json:"id" db:"id"`},
	},
	{
		name:       "a column's json tag is kept when the style omits json tags",
		style:      emit.Style{JSONTags: emit.JSONTags{Omit: true}},
		names:      []string{"email", "id"},
		columnTags: []map[string]string{{"json": "address"}},
		want:       []string{`json:"address"`, ""},
	},
	{
		name:       "a value is quoted as Go quotes it",
		names:      []string{"code"},
		columnTags: []map[string]string{{"pattern": `^"[a-z]+"$`}},
		want:       []string{`json:"code" pattern:"^\"[a-z]+\"$"`},
	},
}

func TestFieldTagsWriteTheJSONTagTheStyleShapesAndTheDeclaredTags(t *testing.T) {
	t.Parallel()

	for _, testCase := range fieldTagCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, emitkit.FieldTags(&testCase.style, testCase.names, testCase.columnTags))
		})
	}
}

func TestFieldTagsNameEveryRepeatedColumnApartInTimeLinearInTheColumns(t *testing.T) {
	t.Parallel()

	repeated := func(count int) []string {
		names := make([]string, count)
		for index := range names {
			names[index] = "x"
		}
		return names
	}
	styles := []struct {
		style emit.Style
		name  string
	}{
		{name: "names as written", style: emit.Style{}},
		{name: "names in camel case", style: emit.Style{JSONTags: emit.JSONTags{Case: emit.JSONCaseCamel}}},
	}

	for _, testCase := range styles {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tags := emitkit.FieldTags(&testCase.style, repeated(3), nil)
			require.Equal(t, []string{`json:"x"`, `json:"x2"`, `json:"x3"`}, tags)

			smaller, larger := repeated(4_000), repeated(16_000)
			scaling.Linear(t,
				func() { emitkit.FieldTags(&testCase.style, smaller, nil) },
				func() { emitkit.FieldTags(&testCase.style, larger, nil) })
		})
	}
}
