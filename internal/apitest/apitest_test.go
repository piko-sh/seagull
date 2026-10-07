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

package apitest

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

type testStruct struct {
	Name    string
	Count   int
	private bool
}

func (sample testStruct) Public() string { return sample.Name }

func (sample *testStruct) SetName(name string) { sample.Name = name }

type testInterface interface {
	DoSomething(value int) (string, error)
	Simple()
}

type recordingTB struct {
	testing.TB
	failures []string
	logs     []string
}

func (recorder *recordingTB) Helper() {}

func (recorder *recordingTB) Errorf(format string, arguments ...any) {
	recorder.failures = append(recorder.failures, fmt.Sprintf(format, arguments...))
}

func (recorder *recordingTB) Fatalf(format string, arguments ...any) {
	recorder.Errorf(format, arguments...)
	recorder.FailNow()
}

func (recorder *recordingTB) FailNow() {
	runtime.Goexit()
}

func (recorder *recordingTB) Logf(format string, arguments ...any) {
	recorder.logs = append(recorder.logs, fmt.Sprintf(format, arguments...))
}

func runCheck(t *testing.T, surface Surface, goldenFile string, record bool) *recordingTB {
	t.Helper()

	recorder := &recordingTB{TB: t}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		check(recorder, surface, goldenFile, record)
	}()
	<-finished
	return recorder
}

func sampleSurface() Surface {
	return Surface{
		"Limit": 3,
		"New":   func(string, ...int) (*testStruct, error) { return nil, nil },
		"Shape": (*testInterface)(nil),
		"Thing": testStruct{},
	}
}

func TestGetEntityType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		entity   any
		expected string
	}{
		{name: "nil", entity: nil, expected: "Nil"},
		{name: "struct", entity: testStruct{}, expected: "Struct"},
		{name: "func", entity: func() {}, expected: "Func"},
		{name: "interface pointer", entity: (*testInterface)(nil), expected: "Interface"},
		{name: "struct pointer", entity: (*testStruct)(nil), expected: "Type"},
		{name: "constant", entity: 3, expected: "Type"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, getEntityType(testCase.entity))
		})
	}
}

func TestFormatType(t *testing.T) {
	t.Parallel()

	t.Run("nil type", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "nil", formatType(nil))
	})

	t.Run("simple type", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "int", formatType(reflect.TypeFor[int]()))
	})

	t.Run("func type", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "func(int) string", formatType(reflect.TypeFor[func(int) string]()))
	})

	t.Run("variadic func type", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "func(string, ...int)", formatType(reflect.TypeFor[func(string, ...int)]()))
	})
}

func TestFormatMethodSignature(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		expected   string
		parameters []string
		results    []string
		isVariadic bool
	}{
		{name: "no parameters or results", expected: "()", parameters: nil, results: nil, isVariadic: false},
		{name: "parameters only", expected: "(int, string)", parameters: []string{"int", "string"}, results: nil, isVariadic: false},
		{name: "results only", expected: "() error", parameters: nil, results: []string{"error"}, isVariadic: false},
		{name: "parameters and result", expected: "(int) string", parameters: []string{"int"}, results: []string{"string"}, isVariadic: false},
		{name: "multiple results", expected: "(int) (string, error)", parameters: []string{"int"}, results: []string{"string", "error"}, isVariadic: false},
		{name: "variadic", expected: "(...string)", parameters: []string{"[]string"}, results: nil, isVariadic: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, formatMethodSignature(testCase.parameters, testCase.results, testCase.isVariadic))
		})
	}
}

func TestFormatMethodSignatureLeavesTheCallersParametersAlone(t *testing.T) {
	t.Parallel()

	parameters := []string{"int", "[]string"}

	assert.Equal(t, "(int, ...string)", formatMethodSignature(parameters, nil, true))
	assert.Equal(t, []string{"int", "[]string"}, parameters)
}

func TestFormatFunction(t *testing.T) {
	t.Parallel()

	t.Run("non-func type", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "int", formatFunction(reflect.TypeFor[int]()))
	})

	t.Run("func no parameters no results", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "()", formatFunction(reflect.TypeFor[func()]()))
	})

	t.Run("func with parameters and results", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "(int, string) error", formatFunction(reflect.TypeFor[func(int, string) error]()))
	})
}

func TestGenerateSignature(t *testing.T) {
	t.Parallel()

	t.Run("nil entity", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "nil", generateSignature(nil))
	})

	t.Run("struct entity", func(t *testing.T) {
		t.Parallel()
		signature := generateSignature(testStruct{})
		assert.Contains(t, signature, "struct {")
		assert.Contains(t, signature, "Name")
		assert.Contains(t, signature, "Count")
		assert.Contains(t, signature, "Public")
	})

	t.Run("func entity", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "func(int) string", generateSignature(func(int) string { return "" }))
	})

	t.Run("interface entity", func(t *testing.T) {
		t.Parallel()
		signature := generateSignature((*testInterface)(nil))
		assert.Contains(t, signature, "interface {")
		assert.Contains(t, signature, "DoSomething")
		assert.Contains(t, signature, "Simple")
	})

	t.Run("other entity", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "*apitest.testStruct", generateSignature((*testStruct)(nil)))
	})
}

func TestGenerateStructSignature(t *testing.T) {
	t.Parallel()

	structType := reflect.TypeFor[testStruct]()
	signature := generateStructSignature(structType, reflect.PointerTo(structType))

	assert.True(t, strings.HasPrefix(signature, "struct {"))
	assert.True(t, strings.HasSuffix(signature, "}"))
	assert.Contains(t, signature, "Name string")
	assert.Contains(t, signature, "Count int")
	assert.Contains(t, signature, "unexported fields")
	assert.Contains(t, signature, "Public() string")
	assert.Contains(t, signature, "SetName(string)")
}

func TestGenerateStructSignatureOfAnEmptyStruct(t *testing.T) {
	t.Parallel()

	structType := reflect.TypeFor[struct{}]()

	assert.Equal(t, "struct {\n}", generateStructSignature(structType, reflect.PointerTo(structType)))
}

func TestGenerateInterfaceSignature(t *testing.T) {
	t.Parallel()

	signature := generateInterfaceSignature(reflect.TypeFor[testInterface]())

	assert.Equal(t, "interface {\n    DoSomething(int) (string, error)\n    Simple()\n}", signature)
}

func TestGenerateAPISnapshotYAMLOfAnEmptySurface(t *testing.T) {
	t.Parallel()

	node := generateAPISnapshotYAML(Surface{})

	require.NotNil(t, node)
	assert.Equal(t, yaml.MappingNode, node.Kind)
	assert.Empty(t, node.Content)
}

func TestGenerateAPISnapshotYAMLSortsTheKeys(t *testing.T) {
	t.Parallel()

	node := generateAPISnapshotYAML(Surface{
		"Zebra": 42,
		"Apple": "hello",
	})

	require.Len(t, node.Content, 4)
	assert.Equal(t, "Apple", node.Content[0].Value)
	assert.Equal(t, "Zebra", node.Content[2].Value)
}

func TestGenerateAPISnapshotYAMLMarksAFunctionWithItsKind(t *testing.T) {
	t.Parallel()

	node := generateAPISnapshotYAML(Surface{
		"MyFunc": func(int) string { return "" },
	})

	require.Len(t, node.Content, 2)
	assert.Equal(t, "MyFunc", node.Content[0].Value)
	assert.Equal(t, "# Kind: Func", node.Content[0].LineComment)
	assert.Equal(t, "func(int) string", node.Content[1].Value)
}

func TestRenderSnapshotWritesSortedEntriesWithTheirKinds(t *testing.T) {
	t.Parallel()

	snapshot, err := renderSnapshot(sampleSurface())

	require.NoError(t, err)
	assert.Equal(t, "# Seagull Public API Surface\n"+
		"\n"+
		"# This file is auto-generated by `go test -update-api`. DO NOT EDIT.\n"+
		"# It is a snapshot of the public API to prevent unintentional breaking changes.\n"+
		"Limit: int # Kind: Type\n"+
		"New: func(string, ...int) (*apitest.testStruct, error) # Kind: Func\n"+
		"Shape: |- # Kind: Interface\n"+
		"  interface {\n"+
		"      DoSomething(int) (string, error)\n"+
		"      Simple()\n"+
		"  }\n"+
		"Thing: |- # Kind: Struct\n"+
		"  struct {\n"+
		"      // Field\n"+
		"      Name string\n"+
		"      // Field\n"+
		"      Count int\n"+
		"\n"+
		"      // (+1 unexported fields)\n"+
		"\n"+
		"      // Method\n"+
		"      Public() string\n"+
		"      // Method\n"+
		"      SetName(string)\n"+
		"  }\n", string(snapshot))
}

func TestCheckPassesWhenTheGoldenFileMatches(t *testing.T) {
	t.Parallel()

	goldenFile := filepath.Join(t.TempDir(), "facade_test.golden.yaml")
	snapshot, err := renderSnapshot(sampleSurface())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(goldenFile, snapshot, 0o600))

	Check(t, sampleSurface(), goldenFile)
}

func TestCheckRecordsTheGoldenFile(t *testing.T) {
	t.Parallel()

	goldenFile := filepath.Join(t.TempDir(), "nested", "facade_test.golden.yaml")

	recorder := runCheck(t, sampleSurface(), goldenFile, true)

	require.Empty(t, recorder.failures)
	require.Equal(t, []string{"API snapshot recorded: " + goldenFile}, recorder.logs)
	recorded, err := os.ReadFile(goldenFile)
	require.NoError(t, err)
	snapshot, err := renderSnapshot(sampleSurface())
	require.NoError(t, err)
	require.Equal(t, string(snapshot), string(recorded))

	again := runCheck(t, sampleSurface(), goldenFile, false)
	require.Empty(t, again.failures)
}

func TestCheckFailsWhenTheSurfaceDrifts(t *testing.T) {
	t.Parallel()

	goldenFile := filepath.Join(t.TempDir(), "facade_test.golden.yaml")
	require.Empty(t, runCheck(t, sampleSurface(), goldenFile, true).failures)

	testCases := []struct {
		change   func(surface Surface)
		name     string
		expected string
	}{
		{
			name:     "a removed member",
			change:   func(surface Surface) { delete(surface, "Shape") },
			expected: "-Shape: |- # Kind: Interface",
		},
		{
			name:     "an added member",
			change:   func(surface Surface) { surface["Extra"] = true },
			expected: "+Extra: bool # Kind: Type",
		},
		{
			name:     "a changed signature",
			change:   func(surface Surface) { surface["New"] = func(string) *testStruct { return nil } },
			expected: "+New: func(string) *apitest.testStruct # Kind: Func",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			surface := sampleSurface()
			testCase.change(surface)

			recorder := runCheck(t, surface, goldenFile, false)

			require.Len(t, recorder.failures, 1)
			assert.Contains(t, recorder.failures[0], testCase.expected)
			assert.Contains(t, recorder.failures[0], "go test -update-api")
		})
	}
}

func TestCheckFailsWhenTheGoldenFileIsMissing(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	testCases := []struct {
		name       string
		goldenFile string
	}{
		{name: "a missing file", goldenFile: filepath.Join(directory, "facade_test.golden.yaml")},
		{name: "a missing directory", goldenFile: filepath.Join(directory, "absent", "facade_test.golden.yaml")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := runCheck(t, sampleSurface(), testCase.goldenFile, false)

			require.Equal(t, []string{"API snapshot " + testCase.goldenFile + " does not exist; record it with go test -update-api"}, recorder.failures)
			assert.NoDirExists(t, filepath.Join(directory, "absent"))
		})
	}
}
