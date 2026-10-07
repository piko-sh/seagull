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
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/gogen"
)

func labellingVocabulary() gogen.Vocabulary {
	request := testRequest()
	request.Style.WrapErrors = true
	return gogen.NewVocabulary(request)
}

func labelled(t *testing.T, source string) string {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "method.go", "package db\n\n"+source, parser.SkipObjectResolution)
	require.NoError(t, err)
	declaration, isFunction := file.Decls[0].(*ast.FuncDecl)
	require.True(t, isFunction)

	LabelErrors(labellingVocabulary(), "GetBoat", declaration)

	var printed bytes.Buffer
	require.NoError(t, printer.Fprint(&printed, fileSet, declaration))
	return printed.String()
}

func TestLabelErrorsLabelsEveryErrorAMethodReturns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "an error returned beside a value",
			source: "func (q *Queries) GetBoat() (Row, error) {\n\tif err != nil {\n\t\treturn Row{}, err\n\t}\n\treturn row, nil\n}",
			want:   "func (q *Queries) GetBoat() (Row, error) {\n\tif err != nil {\n\t\treturn Row{}, sglWrapError(\"sgl: GetBoat\", err)\n\t}\n\treturn row, nil\n}",
		},
		{
			name:   "an error that may be nil",
			source: "func (q *Queries) GetBoat() error {\n\t_, err := q.db.Exec()\n\treturn err\n}",
			want:   "func (q *Queries) GetBoat() error {\n\t_, err := q.db.Exec()\n\treturn sglWrapError(\"sgl: GetBoat\", err)\n}",
		},
		{
			name:   "a call returning only an error",
			source: "func (q *Queries) GetBoat() error {\n\treturn batch.Send()\n}",
			want:   "func (q *Queries) GetBoat() error {\n\treturn sglWrapError(\"sgl: GetBoat\", batch.Send())\n}",
		},
		{
			name:   "a call returning a value beside its error",
			source: "func (q *Queries) GetBoat() (int64, error) {\n\treturn results.RowsAffected()\n}",
			want:   "func (q *Queries) GetBoat() (int64, error) {\n\treturn sglWrapResult[int64](\"sgl: GetBoat\")(results.RowsAffected())\n}",
		},
		{
			name:   "an error beside two values",
			source: "func (q *Queries) GetBoat() (Row, bool, error) {\n\treturn Row{}, false, err\n}",
			want:   "func (q *Queries) GetBoat() (Row, bool, error) {\n\treturn Row{}, false, sglWrapError(\"sgl: GetBoat\", err)\n}",
		},
		{
			name:   "an error already labelled",
			source: "func (q *Queries) GetBoat() error {\n\treturn sglWrapError(\"sgl: GetBoat: the statement starting at row \"+strconv.Itoa(offset), err)\n}",
			want:   "func (q *Queries) GetBoat() error {\n\treturn sglWrapError(\"sgl: GetBoat: the statement starting at row \"+strconv.Itoa(offset), err)\n}",
		},
		{
			name:   "a function literal's own return",
			source: "func (q *Queries) GetBoat() (int64, error) {\n\treturn q.db.CopyFrom(func(i int) ([]any, error) {\n\t\treturn args, err\n\t})\n}",
			want: "func (q *Queries) GetBoat() (int64, error) {\n\treturn sglWrapResult[int64](\"sgl: GetBoat\")(q.db.CopyFrom(func(i int) ([]any, error) {\n" +
				"\t\treturn args, err\n\t}))\n}",
		},
		{
			name:   "an error a stream yields",
			source: "func (q *Queries) GetBoat() func(func(Row, error) bool) {\n\treturn func(yield func(Row, error) bool) {\n\t\tif !yield(Row{}, err) {\n\t\t\treturn\n\t\t}\n\t\tyield(row, nil)\n\t}\n}",
			want: "func (q *Queries) GetBoat() func(func(Row, error) bool) {\n\treturn func(yield func(Row, error) bool) {\n" +
				"\t\tif !yield(Row{}, sglWrapError(\"sgl: GetBoat\", err)) {\n\t\t\treturn\n\t\t}\n\t\tyield(row, nil)\n\t}\n}",
		},
		{
			name:   "a method returning no error",
			source: "func (q *Queries) GetBoat() *Builder {\n\treturn builder\n}",
			want:   "func (q *Queries) GetBoat() *Builder {\n\treturn builder\n}",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, labelled(t, testCase.source))
		})
	}
}

func TestLabelErrorsLeavesADeclarationWithoutABodyAlone(t *testing.T) {
	t.Parallel()

	declaration := &ast.FuncDecl{Name: ast.NewIdent("GetBoat"), Type: &ast.FuncType{}}

	LabelErrors(labellingVocabulary(), "GetBoat", declaration)
	LabelErrors(labellingVocabulary(), "GetBoat", nil)

	require.Nil(t, declaration.Body)
}
