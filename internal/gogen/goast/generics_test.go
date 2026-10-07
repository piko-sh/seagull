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

package goast

import (
	"bytes"
	"go/printer"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestWrittenGoTypeReadsInstancesOfGenericTypes(t *testing.T) {
	t.Parallel()

	identifier := sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}
	option := sqlmodel.GoType{Package: "github.com/x/opt", Name: "Option"}
	optional := sqlmodel.NewGoInstance(option, identifier)
	pointer := sqlmodel.NewGoInstance(sqlmodel.GoType{Package: "example.com/pairs", Name: "Pair"},
		sqlmodel.GoType{Name: "string"}, sqlmodel.GoType{Package: "example.com/y", Name: "[]*Z"})
	pointer.Name = "*"
	nested := sqlmodel.NewGoInstance(option, sqlmodel.NewGoInstance(option, sqlmodel.GoType{Name: "int"}))
	testCases := []struct {
		name        string
		packagePath string
		written     string
		want        sqlmodel.GoType
	}{
		{name: "a named type", packagePath: "github.com/google/uuid", written: "UUID", want: identifier},
		{name: "a slice of a named type", packagePath: "example.com/y", written: "[]Z", want: sqlmodel.GoType{Package: "example.com/y", Name: "[]Z"}},
		{name: "an instance", packagePath: "github.com/x/opt", written: "Option[github.com/google/uuid.UUID]", want: optional},
		{name: "a pointer to an instance of two arguments", packagePath: "example.com/pairs", written: "*Pair[string, []*example.com/y.Z]", want: pointer},
		{name: "an instance of an instance", packagePath: "github.com/x/opt", written: "Option[github.com/x/opt.Option[int]]", want: nested},
		{name: "unbalanced brackets stay a name", packagePath: "example.com/y", written: "Box[int", want: sqlmodel.GoType{Package: "example.com/y", Name: "Box[int"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, WrittenGoType(testCase.packagePath, testCase.written))
		})
	}
}

func TestSplitWrittenTypeCutsAtTheLastDotOutsideTheArguments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		written     string
		packagePath string
		name        string
	}{
		{written: "string", packagePath: "", name: "string"},
		{written: " []*example.com/y.Z ", packagePath: "example.com/y", name: "[]*Z"},
		{written: "gopkg.in/guregu/null.v4.String", packagePath: "gopkg.in/guregu/null.v4", name: "String"},
		{written: "github.com/x/opt.Option[github.com/google/uuid.UUID]", packagePath: "github.com/x/opt", name: "Option[github.com/google/uuid.UUID]"},
		{written: "Box[example.com/y.Z]", packagePath: "", name: "Box[example.com/y.Z]"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.written, func(t *testing.T) {
			t.Parallel()
			packagePath, name := SplitWrittenType(testCase.written)
			require.Equal(t, testCase.packagePath, packagePath)
			require.Equal(t, testCase.name, name)
		})
	}
}

func TestImportTrackerWritesAnInstanceWithItsArguments(t *testing.T) {
	t.Parallel()

	tracker := NewPlannedImportTracker(map[string]string{"github.com/x/opt": "opt", "github.com/google/uuid": "uuid"})
	instance := WrittenGoType("github.com/x/opt", "*Option[github.com/google/uuid.UUID]")
	var written bytes.Buffer
	require.NoError(t, printer.Fprint(&written, token.NewFileSet(), tracker.AddType(instance)))
	require.Equal(t, "*opt.Option[uuid.UUID]", written.String())

	pair := WrittenGoType("example.com/pairs", "Pair[string, int]")
	written.Reset()
	require.NoError(t, printer.Fprint(&written, token.NewFileSet(), tracker.AddType(pair)))
	require.Equal(t, "pairs.Pair[string, int]", written.String())
}
