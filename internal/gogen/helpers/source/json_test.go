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
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scannedDocument struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func TestScanIntoDecodesAJSONColumnIntoItsDestination(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		source any
		name   string
		want   scannedDocument
	}{
		{
			name:   "bytes",
			source: []byte(`{"name":"anchor","tags":["blue"]}`),
			want:   scannedDocument{Name: "anchor", Tags: []string{"blue"}},
		},
		{
			name:   "text",
			source: `{"name":"buoy","tags":[]}`,
			want:   scannedDocument{Name: "buoy", Tags: []string{}},
		},
		{
			name:   "an already decoded value",
			source: map[string]any{"name": "cleat", "tags": []any{"red", "green"}},
			want:   scannedDocument{Name: "cleat", Tags: []string{"red", "green"}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var destination scannedDocument
			require.NoError(t, sglScanInto(&destination).Scan(testCase.source))
			require.Equal(t, testCase.want, destination)
		})
	}
}

func TestScanIntoResetsTheDestinationForANullOrEmptyColumn(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]any{
		"null":        nil,
		"empty bytes": []byte{},
		"empty text":  "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			destination := scannedDocument{Name: "stale", Tags: []string{"old"}}
			require.NoError(t, sglScanInto(&destination).Scan(source))
			require.Zero(t, destination)
		})
	}
}

func TestScanIntoReadsANullElementOnlyIntoATypeThatHoldsIt(t *testing.T) {
	t.Parallel()

	var pointers []*int32
	require.NoError(t, sglScanInto(&pointers).Scan([]byte(`[1,null,3]`)))
	require.Len(t, pointers, 3)
	require.Nil(t, pointers[1])
	require.Equal(t, int32(3), *pointers[2])

	var nested [][]*string
	require.NoError(t, sglScanInto(&nested).Scan([]byte(`[["a",null],null]`)))
	require.Nil(t, nested[0][1])
	require.Nil(t, nested[1])

	var document scannedDocument
	require.NoError(t, sglScanInto(&document).Scan([]byte(`{"name":null,"tags":null}`)))

	testCases := []struct {
		name   string
		source string
		read   func(source string) error
		want   string
	}{
		{name: "an integer element", source: `[1,null]`, read: scanInto[[]int32], want: "element 1: converting NULL to int32 is unsupported"},
		{name: "a nested text element", source: `[["a"],["b",null]]`, read: scanInto[[][]string], want: "element 1: element 1: converting NULL to string is unsupported"},
		{name: "a map value", source: `{"k":null}`, read: scanInto[map[string]float64], want: "value of key k: converting NULL to float64 is unsupported"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.read(testCase.source)
			require.ErrorContains(t, err, "sgl: decode JSON column: ")
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func scanInto[T any](source string) error {
	var destination T
	return sglScanInto(&destination).Scan([]byte(source))
}

func TestScanIntoReportsAColumnThatIsNotValidJSON(t *testing.T) {
	t.Parallel()

	var destination scannedDocument
	err := sglScanInto(&destination).Scan([]byte(`{"name":`))

	var syntaxError *json.SyntaxError
	require.ErrorAs(t, err, &syntaxError)
	require.ErrorContains(t, err, "sgl: decode JSON column")
}

func TestScanIntoReportsAValueThatCannotBeReEncoded(t *testing.T) {
	t.Parallel()

	var destination scannedDocument
	err := sglScanInto(&destination).Scan(make(chan int))

	var unsupported *json.UnsupportedTypeError
	require.ErrorAs(t, err, &unsupported)
	require.ErrorContains(t, err, "sgl: re-encode chan int to JSON")
}

func TestJSONScanReadsTheShapesDriversReturn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		source any
		name   string
		want   JSON
	}{
		{name: "null", source: nil, want: nil},
		{name: "bytes", source: []byte(`{"a":1}`), want: JSON(`{"a":1}`)},
		{name: "text", source: `[1,2]`, want: JSON(`[1,2]`)},
		{name: "an already decoded map", source: map[string]any{"a": 1}, want: JSON(`{"a":1}`)},
		{name: "an already decoded number", source: 7, want: JSON(`7`)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			document := JSON(`"stale"`)
			require.NoError(t, document.Scan(testCase.source))
			require.Equal(t, testCase.want, document)
		})
	}
}

type decodedDocument struct {
	text string
}

func (document *decodedDocument) MarshalJSON() ([]byte, error) {
	return []byte(document.text), nil
}

func TestJSONScanEncodesADecodedDocumentAsJSONMarshalWritesIt(t *testing.T) {
	t.Parallel()

	source := &decodedDocument{text: `{"a":"<b>","n":1}`}
	want, err := json.Marshal(source)
	require.NoError(t, err)

	var document JSON
	require.NoError(t, document.Scan(source))

	require.Equal(t, JSON(want), document, "a ClickHouse JSON column read through database/sql gives the bytes the converting adapters give")
}

func TestJSONScanCopiesBytesRatherThanKeepingTheDriversBuffer(t *testing.T) {
	t.Parallel()

	buffer := []byte(`{"a":1}`)
	var document JSON
	require.NoError(t, document.Scan(buffer))

	copy(buffer, `{"b":2}`)

	require.Equal(t, JSON(`{"a":1}`), document)
}

func TestJSONScanReportsAValueThatCannotBeEncoded(t *testing.T) {
	t.Parallel()

	var document JSON
	err := document.Scan(func() {})

	var unsupported *json.UnsupportedTypeError
	require.ErrorAs(t, err, &unsupported)
	require.ErrorContains(t, err, "sgl: encode func() to JSON")
}

func TestJSONValueWritesTheDocumentAsText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		want     driver.Value
		name     string
		document JSON
	}{
		{name: "a nil document is NULL", document: nil, want: nil},
		{name: "a document is its text", document: JSON(`{"a":1}`), want: `{"a":1}`},
		{name: "the text keeps every byte", document: JSON("{\"b\": [1, 2.50], \"a\": \"<\u00e9 & \U0001F600>\"}"),
			want: "{\"b\": [1, 2.50], \"a\": \"<\u00e9 & \U0001F600>\"}"},
		{name: "an empty document is empty text", document: JSON{}, want: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, err := testCase.document.Value()
			require.NoError(t, err)
			require.Equal(t, testCase.want, value)
		})
	}
}

func TestJSONSurvivesAWriteAndReadThroughADriver(t *testing.T) {
	t.Parallel()

	original := JSON(`{"nested":{"list":[1,2,3]}}`)
	written, err := original.Value()
	require.NoError(t, err)

	var read JSON
	require.NoError(t, read.Scan(written))

	require.Equal(t, original, read)
}

func TestJSONMarshalsAsTheRawDocumentItHolds(t *testing.T) {
	t.Parallel()

	type envelope struct {
		Document JSON `json:"document"`
	}

	testCases := []struct {
		name     string
		document JSON
		want     string
	}{
		{name: "a document", document: JSON(`{"a":[1,2]}`), want: `{"document":{"a":[1,2]}}`},
		{name: "a nil document", document: nil, want: `{"document":null}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(envelope{Document: testCase.document})
			require.NoError(t, err)
			require.JSONEq(t, testCase.want, string(encoded))
		})
	}
}

func TestJSONUnmarshalKeepsACopyOfTheRawDocument(t *testing.T) {
	t.Parallel()

	type envelope struct {
		Document JSON `json:"document"`
	}

	var decoded envelope
	require.NoError(t, json.Unmarshal([]byte(`{"document":{"a":[1, 2]}}`), &decoded))
	require.Equal(t, JSON(`{"a":[1, 2]}`), decoded.Document)

	buffer := []byte(`{"b":2}`)
	var document JSON
	require.NoError(t, document.UnmarshalJSON(buffer))
	copy(buffer, `{"c":3}`)
	require.Equal(t, JSON(`{"b":2}`), document)
}

type textDecimal string

func (d *textDecimal) UnmarshalText(text []byte) error {
	*d = textDecimal(text)
	return nil
}

func TestScanIntoReadsTheElementsAnEngineWritesInItsOwnForm(t *testing.T) {
	t.Parallel()

	moment := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC)
	day := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	var timestamps []time.Time
	require.NoError(t, sglScanInto(&timestamps).Scan([]byte(`["2024-01-02T03:04:05.123456","2024-01-02T00:00:00"]`)))
	require.Equal(t, []time.Time{moment, day}, timestamps)

	var dates []*time.Time
	require.NoError(t, sglScanInto(&dates).Scan([]byte(`["2024-01-02",null]`)))
	require.Equal(t, []*time.Time{&day, nil}, dates)

	var zoned []time.Time
	require.NoError(t, sglScanInto(&zoned).Scan([]byte(`["2024-01-02T03:04:05+02:00"]`)))
	require.True(t, zoned[0].Equal(time.Date(2024, 1, 2, 1, 4, 5, 0, time.UTC)))

	var grid [][]time.Time
	require.NoError(t, sglScanInto(&grid).Scan([]byte(`[["2024-01-02"]]`)))
	require.Equal(t, [][]time.Time{{day}}, grid)

	var amounts []textDecimal
	require.NoError(t, sglScanInto(&amounts).Scan([]byte(`[1.50,2,-0.001]`)))
	require.Equal(t, []textDecimal{"1.50", "2", "-0.001"}, amounts)

	var texts []*string
	require.NoError(t, sglScanInto(&texts).Scan([]byte(`[1.50,null]`)))
	require.Equal(t, "1.50", *texts[0])
	require.Nil(t, texts[1])

	var optional []*textDecimal
	require.NoError(t, sglScanInto(&optional).Scan([]byte(`[null,10.00]`)))
	require.Nil(t, optional[0])
	require.Equal(t, textDecimal("10.00"), *optional[1])

	var blobs [][]byte
	require.NoError(t, sglScanInto(&blobs).Scan([]byte(`["\\x0102ff","\\x"]`)))
	require.Equal(t, [][]byte{{0x01, 0x02, 0xff}, {}}, blobs)

	var documents []json.RawMessage
	require.NoError(t, sglScanInto(&documents).Scan([]byte(`[{"a":1},"\\x01"]`)))
	require.Equal(t, []json.RawMessage{json.RawMessage(`{"a":1}`), json.RawMessage(`"\\x01"`)}, documents)
}

func TestScanIntoStillReportsAnElementItCannotRead(t *testing.T) {
	t.Parallel()

	var timestamps []time.Time
	require.ErrorContains(t, sglScanInto(&timestamps).Scan([]byte(`["yesterday"]`)), "sgl: decode JSON column")

	var blobs [][]byte
	require.ErrorContains(t, sglScanInto(&blobs).Scan([]byte(`["\\xzz"]`)), "sgl: decode JSON column")
}

var trailingDataCases = []struct {
	name   string
	source string
	read   func(source string) error
}{
	{name: "a second array after a list of integers", source: `[1] [2]`, read: scanInto[[]int64]},
	{name: "a closing bracket after a list of integers", source: `[1]]`, read: scanInto[[]int64]},
	{name: "a second object after a document", source: `{"name":"a"} {}`, read: scanInto[scannedDocument]},
	{name: "a word after dates an engine wrote", source: `["2024-01-02"] later`, read: scanInto[[]time.Time]},
	{name: "a second array after numbers read as text", source: `[1.50] [2]`, read: scanInto[[]string]},
	{name: "a number after a number", source: `1 2`, read: scanInto[int64]},
}

func TestScanIntoReportsADocumentFollowedByMoreData(t *testing.T) {
	t.Parallel()

	for _, testCase := range trailingDataCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorContains(t, testCase.read(testCase.source), "sgl: decode JSON column")
		})
	}
}

func TestScanIntoLeavesTheDestinationEmptyWhenMoreDataFollows(t *testing.T) {
	t.Parallel()

	destination := []int64{7}
	require.Error(t, sglScanInto(&destination).Scan([]byte(`[1] [2]`)))
	require.Nil(t, destination)

	var spaced []int64
	require.NoError(t, sglScanInto(&spaced).Scan([]byte(" [1, 2] \n\t")))
	require.Equal(t, []int64{1, 2}, spaced)
}

var pointerNullCases = []struct {
	name   string
	source string
	read   func(source string) error
	want   string
}{
	{name: "a list behind a pointer", source: `[1,null]`, read: scanInto[*[]int64], want: "element 1: converting NULL to int64 is unsupported"},
	{name: "a list behind two pointers", source: `[null]`, read: scanInto[**[]string], want: "element 0: converting NULL to string is unsupported"},
	{name: "lists behind pointers in a list", source: `[[1],[2,null]]`, read: scanInto[[]*[]int32], want: "element 1: element 1: converting NULL to int32 is unsupported"},
	{name: "a map behind a pointer", source: `{"k":null}`, read: scanInto[*map[string]float64], want: "value of key k: converting NULL to float64 is unsupported"},
	{name: "a list a number wider than a float64 leads", source: `[1e400,null]`, read: scanInto[[]textDecimal], want: "element 1: converting NULL to source.textDecimal is unsupported"},
}

func TestScanIntoLooksThroughPointersForNullElements(t *testing.T) {
	t.Parallel()

	for _, testCase := range pointerNullCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.read(testCase.source)
			require.ErrorContains(t, err, "sgl: decode JSON column: ")
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func TestScanIntoReadsNullElementsBehindPointersIntoTypesThatHoldThem(t *testing.T) {
	t.Parallel()

	var list *[]*int64
	require.NoError(t, sglScanInto(&list).Scan([]byte(`[1,null]`)))
	require.NotNil(t, list)
	require.Len(t, *list, 2)
	require.Nil(t, (*list)[1])

	var lists []*[]int32
	require.NoError(t, sglScanInto(&lists).Scan([]byte(`[null,[3]]`)))
	require.Nil(t, lists[0])
	require.Equal(t, []int32{3}, *lists[1])

	var wide []textDecimal
	require.NoError(t, sglScanInto(&wide).Scan([]byte(`[1e400]`)))
	require.Equal(t, []textDecimal{"1e400"}, wide)
}
