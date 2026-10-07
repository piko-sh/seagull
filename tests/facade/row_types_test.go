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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

const modelRowQueries = `-- sgl.query(ListPosts, many, model: posts)
SELECT id, title, author_id FROM posts;

-- sgl.query(FirstPost, one, model: posts)
SELECT title, author_id, id FROM posts ORDER BY id LIMIT 1;

-- sgl.query(StreamPosts, stream, model: posts)
SELECT id, title, author_id FROM posts;

-- sgl.query(SearchPosts, many, dynamic: runtime, model: posts)
SELECT id, title, author_id FROM posts;
`

const sharedRowQueries = `-- sgl.query(ListTitles, many, row: post_title)
SELECT id, title FROM posts;

-- sgl.query(FirstTitle, one, row: post_title)
SELECT title, id FROM posts ORDER BY id LIMIT 1;
`

const reusedModelQueries = `-- sgl.query(ListPosts, many)
SELECT id, title, author_id FROM posts;

-- sgl.query(ListTitles, many)
SELECT id, title FROM posts;
`

func TestQueriesReadTheirRowsIntoTheStructTheirHeaderNames(t *testing.T) {
	t.Parallel()

	for _, target := range featureTargets {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()

			models := generateFeatureProject(t, target, modelRowQueries, seagull.DefaultPreset())["posts.sql.go"]
			require.Contains(t, models, ") ListPosts(ctx context.Context) ([]Posts, error)")
			require.Contains(t, models, ") FirstPost(ctx context.Context) (Posts, error)")
			require.Contains(t, models, ") StreamPosts(ctx context.Context) iter.Seq2[Posts, error]")
			require.Contains(t, models, ") All(ctx context.Context) ([]Posts, error)")
			require.NotContains(t, models, "Row struct")

			shared := generateFeatureProject(t, target, sharedRowQueries, seagull.DefaultPreset())["posts.sql.go"]
			require.Equal(t, 1, strings.Count(shared, "type PostTitle struct"))
			require.Contains(t, shared, ") ListTitles(ctx context.Context) ([]PostTitle, error)")
			require.Contains(t, shared, ") FirstTitle(ctx context.Context) (PostTitle, error)")
			require.NotContains(t, shared, "Row struct")

			reused := seagull.DefaultPreset()
			reused.ReuseModels = true
			reusing := generateFeatureProject(t, target, reusedModelQueries, reused)["posts.sql.go"]
			require.Contains(t, reusing, ") ListPosts(ctx context.Context) ([]Posts, error)")
			require.Contains(t, reusing, ") ListTitles(ctx context.Context) ([]ListTitlesRow, error)")

			own := generateFeatureProject(t, target, reusedModelQueries, seagull.DefaultPreset())["posts.sql.go"]
			require.Contains(t, own, ") ListPosts(ctx context.Context) ([]ListPostsRow, error)")
		})
	}
}

func TestQueriesWhoseRowsDoNotFitTheStructTheyNameAreReported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		queries string
		message string
	}{
		{
			name:    "a model missing from the columns",
			queries: "-- sgl.query(ListTitles, many, model: posts)\nSELECT id, title FROM posts;\n",
			message: "model: the rows do not fit the model Posts of posts: no column gives the field AuthorID of the model Posts",
		},
		{
			name: "a shared row type the columns of a later query do not give",
			queries: "-- sgl.query(ListTitles, many, row: PostTitle)\nSELECT id, title FROM posts;\n\n" +
				"-- sgl.query(ListAuthors, many, row: PostTitle)\nSELECT id, author_id FROM posts;\n",
			message: "row: the rows do not fit PostTitle, which ListAuthors declares",
		},
		{
			name:    "a table the schema does not hold",
			queries: "-- sgl.query(ListTitles, many, model: articles)\nSELECT id, title FROM posts;\n",
			message: "model: the schema holds no table articles",
		},
		{
			name:    "a query that returns no rows",
			queries: "-- sgl.query(DeletePosts, exec, model: posts)\nDELETE FROM posts;\n",
			message: "model: names the struct a query's rows are read into, and the exec command returns no rows",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			target := featureTargets[0]
			generator, err := seagull.New(target.engine(), seagull.WithEmitter(target.emitter()))
			require.NoError(t, err)
			result, err := generator.Generate(t.Context(), featureProjectInput(target, test.queries))
			require.NoError(t, err)
			require.Len(t, result.Diagnostics, 1)
			require.Equal(t, seagull.CodeRowType, result.Diagnostics[0].Code)
			require.Equal(t, seagull.SeverityError, result.Diagnostics[0].Severity)
			require.Contains(t, result.Diagnostics[0].Message, test.message)
		})
	}
}
