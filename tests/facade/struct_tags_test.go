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

package facade_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

const tagQueries = `-- sgl.query(ListPosts, many)
SELECT id, title, author_id FROM posts;
`

var tagStyleCases = []struct {
	shape  func(*seagull.Preset)
	name   string
	model  []string
	row    []string
	absent []string
}{
	{
		name:  "the default writes the column's name as it is written",
		shape: func(*seagull.Preset) {},
		model: []string{"`json:\"id\"`", "`json:\"title\"`", "`json:\"author_id\"`"},
		row:   []string{"`json:\"id\"`", "`json:\"author_id\"`"},
	},
	{
		name: "camel case with omitempty",
		shape: func(chosen *seagull.Preset) {
			chosen.JSONTags = seagull.JSONTags{Case: seagull.JSONCaseCamel, Omit: false, OmitEmpty: true}
		},
		model:  []string{"`json:\"id,omitempty\"`", "`json:\"authorId,omitempty\"`"},
		row:    []string{"`json:\"authorId,omitempty\"`"},
		absent: []string{"json:\"author_id"},
	},
	{
		name:   "no json tags",
		shape:  func(chosen *seagull.Preset) { chosen.JSONTags.Omit = true },
		absent: []string{"json:"},
	},
	{
		name: "project-wide struct tags after json",
		shape: func(chosen *seagull.Preset) {
			chosen.StructTags = map[string]string{"db": "{name}", "yaml": "{name},omitempty"}
		},
		model: []string{"`json:\"author_id\" db:\"author_id\" yaml:\"author_id,omitempty\"`"},
		row:   []string{"`json:\"title\" db:\"title\" yaml:\"title,omitempty\"`"},
	},
}

func TestJSONTagOptionsAndStructTagsShapeEveryFieldHoldingAColumn(t *testing.T) {
	t.Parallel()

	for _, target := range featureTargets {
		for _, testCase := range tagStyleCases {
			t.Run(target.name+"/"+testCase.name, func(t *testing.T) {
				t.Parallel()

				chosen := seagull.DefaultPreset()
				testCase.shape(&chosen)
				generated := generateFeatureProject(t, target, tagQueries, chosen)

				for _, tag := range testCase.model {
					require.Contains(t, generated["models.go"], tag)
				}
				for _, tag := range testCase.row {
					require.Contains(t, generated["posts.sql.go"], tag)
				}
				for _, text := range testCase.absent {
					require.NotContains(t, generated["models.go"], text)
					require.NotContains(t, generated["posts.sql.go"], text)
				}
			})
		}
	}
}

func TestAColumnOverrideGivesItsColumnsFieldsStructTags(t *testing.T) {
	t.Parallel()

	for _, target := range featureTargets {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()

			chosen := seagull.DefaultPreset()
			chosen.StructTags = map[string]string{"db": "{name}"}
			overrides := seagull.WithTypeOverrides(
				seagull.TypeOverride{Column: "posts.title", StructTags: map[string]string{"validate": "required", "db": "headline"}},
				seagull.TypeOverride{Column: "posts.author_id", StructTags: map[string]string{"json": "-"}},
			)
			generated := generateFeatureProject(t, target, tagQueries, chosen, overrides)

			for _, file := range []string{"models.go", "posts.sql.go"} {
				require.Contains(t, generated[file], "`json:\"title\" db:\"headline\" validate:\"required\"`")
				require.Contains(t, generated[file], "`json:\"-\" db:\"author_id\"`")
				require.Contains(t, generated[file], "`json:\"id\" db:\"id\"`")
			}
		})
	}
}

func TestColumnDirectivesGiveStructTagsOverTheConfiguredOnes(t *testing.T) {
	t.Parallel()

	target := featureTargets[0]
	target.migration += "-- sgl.column(posts.title, struct_tags: 'validate:\"required,max=200\"')\n"
	queries := "-- sgl.query(ListPosts, many)\n" +
		"-- sgl.column(author_id, struct_tags: 'json:\"author\" binding:\"required\"')\n" +
		"SELECT id, title, author_id FROM posts;\n"
	overrides := seagull.WithTypeOverrides(seagull.TypeOverride{Column: "posts.title", StructTags: map[string]string{"validate": "required", "db": "title"}})

	generated := generateFeatureProject(t, target, queries, seagull.DefaultPreset(), overrides)

	require.Contains(t, generated["models.go"], "Title    string `json:\"title\" db:\"title\" validate:\"required,max=200\"`")
	require.Contains(t, generated["models.go"], "AuthorID *int64 `json:\"author_id\"`")
	require.Contains(t, generated["posts.sql.go"], "Title    string `json:\"title\" db:\"title\" validate:\"required,max=200\"`")
	require.Contains(t, generated["posts.sql.go"], "AuthorID *int64 `json:\"author\" binding:\"required\"`")
}

func TestStructTagsAGeneratedFieldCannotCarryAreRefused(t *testing.T) {
	t.Parallel()

	target := featureTargets[0]
	for _, testCase := range []struct {
		name     string
		override seagull.TypeOverride
		problem  string
	}{
		{name: "tags on a SQL type override", override: seagull.TypeOverride{SQLTypeName: "text", StructTags: map[string]string{"validate": "required"}}, problem: "only a column override takes them"},
		{name: "a key holding a space", override: seagull.TypeOverride{Column: "posts.title", StructTags: map[string]string{"my key": "x"}}, problem: `struct tag key "my key"`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := seagull.New(target.engine(), seagull.WithTypeOverrides(testCase.override))

			require.ErrorIs(t, err, seagull.ErrInvalidConfig)
			require.ErrorContains(t, err, testCase.problem)
		})
	}

	chosen := seagull.DefaultPreset()
	chosen.StructTags = map[string]string{"json": "{name}"}
	_, err := seagull.New(target.engine(), seagull.WithPreset(chosen))
	require.ErrorIs(t, err, seagull.ErrInvalidConfig)
}

func TestAStructTagsDirectiveThatIsNoTagIsReported(t *testing.T) {
	t.Parallel()

	target := featureTargets[0]
	target.migration += "-- sgl.column(posts.title, struct_tags: 'required')\n"
	queries := "-- sgl.query(ListPosts, many)\n-- sgl.column(id, struct_tags: 'db:\"a\" db:\"b\"')\nSELECT id, title FROM posts;\n"
	generator, err := seagull.New(target.engine(), seagull.WithEmitter(target.emitter()))
	require.NoError(t, err)

	report, err := generator.Vet(t.Context(), featureProjectInput(target, queries))

	require.NoError(t, err)
	var messages []string
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == seagull.CodeInvalidKeywordArgumentValue {
			messages = append(messages, diagnostic.Message)
		}
	}
	require.Len(t, messages, 2)
	require.Contains(t, messages[0]+messages[1], "struct_tags for posts.title")
	require.Contains(t, messages[0]+messages[1], `struct_tags for column "id"`)
}
