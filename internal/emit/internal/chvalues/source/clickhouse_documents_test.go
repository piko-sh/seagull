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

package source

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

const documentText = `{"a":"<é & 😀>","b":[1,2.5]}`

const documentMarshalled = "{\"a\":\"\\u003cé \\u0026 😀\\u003e\",\"b\":[1,2.5]}"

type documentScanner struct {
	read any
}

func (scanner *documentScanner) Scan(source any) error {
	scanner.read = source
	return nil
}

type documentFields struct {
	A string    `json:"a"`
	B []float64 `json:"b"`
}

type documentRefusal struct{}

func (*documentRefusal) UnmarshalJSON([]byte) error {
	return errors.New("refused")
}

func document() *sglClickHouseDocument {
	return &sglClickHouseDocument{text: documentText}
}

func readDocuments(t *testing.T, target any, source any) bool {
	t.Helper()
	read, err := sglReadClickHouseDocuments(reflect.ValueOf(target).Elem(), source)
	require.NoError(t, err)
	return read
}

func TestReadClickHouseDocumentsReadsADocumentAsTheTextJSONMarshalWrites(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		read   func(t *testing.T, source any) any
		name   string
		source any
	}{
		{name: "into the JSON type, from a pointer", source: document(), read: func(t *testing.T, source any) any {
			var target sglClickHouseJSON
			require.True(t, readDocuments(t, &target, source))
			return string(target)
		}},
		{name: "into the JSON type, from a value", source: *document(), read: func(t *testing.T, source any) any {
			var target sglClickHouseJSON
			require.True(t, readDocuments(t, &target, source))
			return string(target)
		}},
		{name: "into a json.RawMessage", source: document(), read: func(t *testing.T, source any) any {
			var target json.RawMessage
			require.True(t, readDocuments(t, &target, source))
			return string(target)
		}},
		{name: "into a string", source: document(), read: func(t *testing.T, source any) any {
			var target string
			require.True(t, readDocuments(t, &target, source))
			return target
		}},
		{name: "into a pointer", source: document(), read: func(t *testing.T, source any) any {
			var target *sglClickHouseJSON
			require.True(t, readDocuments(t, &target, source))
			return string(*target)
		}},
		{name: "into an interface, as the JSON type", source: document(), read: func(t *testing.T, source any) any {
			var target any
			require.True(t, readDocuments(t, &target, source))
			return string(target.(sglClickHouseJSON))
		}},
		{name: "into a sql.Scanner, as bytes", source: document(), read: func(t *testing.T, source any) any {
			var target documentScanner
			require.True(t, readDocuments(t, &target, source))
			return string(target.read.([]byte))
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, documentMarshalled, testCase.read(t, testCase.source))
		})
	}
}

func TestReadClickHouseDocumentsDecodesADocumentIntoAnyOtherType(t *testing.T) {
	t.Parallel()

	var target documentFields
	require.True(t, readDocuments(t, &target, document()))

	require.Equal(t, documentFields{A: "<é & 😀>", B: []float64{1, 2.5}}, target)
}

func TestReadClickHouseDocumentsReportsATargetThatRefusesTheDocument(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		target any
		name   string
	}{
		{name: "an unmarshaler's error", target: &documentRefusal{}},
		{name: "a type the document does not decode into", target: new(int64)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := sglReadClickHouseDocuments(reflect.ValueOf(testCase.target).Elem(), document())

			require.True(t, read)
			require.Error(t, err)
		})
	}
}

func TestReadClickHouseDocumentsLeavesAValueHoldingNoDocumentToTheCaller(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		target any
		source any
		name   string
	}{
		{name: "a number", target: new(int64), source: int64(7)},
		{name: "text", target: new(sglClickHouseJSON), source: documentText},
		{name: "a nil document", target: new(sglClickHouseJSON), source: (*sglClickHouseDocument)(nil)},
		{name: "a list of documents the target does not hold as it is", target: new([]sglClickHouseJSON), source: []sglClickHouseDocument{*document()}},
		{name: "a map of numbers", target: new(map[string]int64), source: map[string]int64{"n": 1}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.False(t, readDocuments(t, testCase.target, testCase.source))
		})
	}
}

func TestReadClickHouseDocumentsReadsTheDocumentsAnInterfaceHolds(t *testing.T) {
	t.Parallel()

	source := map[string]any{
		"doc":     document(),
		"n":       int64(1),
		"missing": (*sglClickHouseDocument)(nil),
		"list":    []sglClickHouseDocument{*document()},
		"keyed":   map[string]*sglClickHouseDocument{"k": document(), "none": nil},
		"inner":   map[string]any{"doc": *document()},
		"unnamed": []any{*document(), "text"},
	}
	var target map[string]any

	require.True(t, readDocuments(t, &target, source))

	require.Equal(t, map[string]any{
		"doc":     sglClickHouseJSON(documentMarshalled),
		"n":       int64(1),
		"missing": sglClickHouseJSON(nil),
		"list":    []sglClickHouseJSON{sglClickHouseJSON(documentMarshalled)},
		"keyed":   map[string]sglClickHouseJSON{"k": sglClickHouseJSON(documentMarshalled), "none": nil},
		"inner":   map[string]any{"doc": sglClickHouseJSON(documentMarshalled)},
		"unnamed": []any{sglClickHouseJSON(documentMarshalled), "text"},
	}, target)
	require.IsType(t, &sglClickHouseDocument{}, source["doc"], "the driver's value is left as it was")
}

func TestReadClickHouseDocumentsKeepsANilContainer(t *testing.T) {
	t.Parallel()

	var target []any
	require.True(t, readDocuments(t, &target, []any(nil)))

	require.Nil(t, target)
}
