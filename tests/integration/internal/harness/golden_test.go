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

package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrettyJSONIndentsAndEndsWithANewline(t *testing.T) {
	t.Parallel()

	pretty, err := PrettyJSON([]byte(`{"b":[1,2],"a":"x"}`))
	require.NoError(t, err)
	require.Equal(t, "{\n  \"a\": \"x\",\n  \"b\": [\n    1,\n    2\n  ]\n}\n", string(pretty))

	_, err = PrettyJSON([]byte("not json"))
	require.Error(t, err)
}

func TestNormaliseFieldsReplacesGeneratedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		normalise func(value any)
		name      string
		document  string
		want      string
	}{
		{
			name:      "numeric identifiers become zero at any depth",
			normalise: NormaliseFields(float64(0), true, "ID", "id"),
			document:  `{"ID":17,"rows":[{"id":4,"name":"a"},{"id":"kept","name":"b"}]}`,
			want:      `{"ID":0,"rows":[{"id":0,"name":"a"},{"id":"kept","name":"b"}]}`,
		},
		{
			name:      "named values of any kind are replaced",
			normalise: NormaliseFields("", false, "JobID", "ChunkName"),
			document:  `{"JobID":1001,"ChunkName":"_hyper_1_2_chunk","Rows":3}`,
			want:      `{"JobID":"","ChunkName":"","Rows":3}`,
		},
		{
			name:      "nil leaves the document as it is",
			normalise: nil,
			document:  `{"ID":17}`,
			want:      `{"ID":17}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			normalised, err := Normalised([]byte(test.document), test.normalise)
			require.NoError(t, err)
			require.JSONEq(t, test.want, string(normalised))
		})
	}
}

func TestPrettyJSONKeepsTheDigitsOfEveryNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
		want     string
	}{
		{name: "an unsigned 64-bit integer past a float64's precision", document: `{"big_id":17999999999999999999}`,
			want: "{\n  \"big_id\": 17999999999999999999\n}\n"},
		{name: "the largest signed 64-bit integer", document: `[9223372036854775807,9223372036854775806]`,
			want: "[\n  9223372036854775807,\n  9223372036854775806\n]\n"},
		{name: "a decimal written with its scale", document: `{"price":12.50}`, want: "{\n  \"price\": 12.50\n}\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pretty, err := PrettyJSON([]byte(test.document))
			require.NoError(t, err)
			require.Equal(t, test.want, string(pretty))
		})
	}
}

func TestNormalisedDocumentsDifferWhenTheirIntegersDiffer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		golden   string
		output   string
		wantSame bool
	}{
		{name: "a rounded unsigned integer", golden: `{"big_id":18000000000000000000}`, output: `{"big_id":17999999999999999999}`, wantSame: false},
		{name: "a signed integer one below the maximum", golden: `{"id":9223372036854775807}`, output: `{"id":9223372036854775806}`, wantSame: false},
		{name: "the same integer", golden: `{"id":9223372036854775807}`, output: "{\n  \"id\": 9223372036854775807\n}\n", wantSame: true},
		{name: "members in another order", golden: `{"a":1,"b":2}`, output: `{"b":2,"a":1}`, wantSame: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			golden, err := Normalised([]byte(test.golden), nil)
			require.NoError(t, err)
			output, err := Normalised([]byte(test.output), nil)
			require.NoError(t, err)
			require.Equal(t, test.wantSame, string(golden) == string(output))
		})
	}
}

func TestNormalisedRefusesWhatIsNotOneDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
	}{
		{name: "not JSON", document: "not json"},
		{name: "a second document", document: `{"id":1} {"id":2}`},
		{name: "text after the document", document: `{"id":1} trailing`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Normalised([]byte(test.document), nil)
			require.Error(t, err)
			_, err = PrettyJSON([]byte(test.document))
			require.Error(t, err)
		})
	}
}

func TestNormaliseFieldsTreatsADecodedNumberAsANumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		replacement any
		name        string
		document    string
		want        string
	}{
		{name: "a float64 replacement matches a decoded number", replacement: float64(0), document: `{"id":1180739088078503937}`, want: `{"id":0}`},
		{name: "a json.Number replacement matches a decoded number", replacement: json.Number("0"), document: `{"id":1180739088078503937}`, want: `{"id":0}`},
		{name: "a number replacement leaves a string alone", replacement: float64(0), document: `{"id":"kept"}`, want: `{"id":"kept"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			normalised, err := Normalised([]byte(test.document), NormaliseFields(test.replacement, true, "id"))
			require.NoError(t, err)
			require.JSONEq(t, test.want, string(normalised))
		})
	}
}

func TestAssertGoldenRecordsTheOutputAndThenMatchesIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		normalise func(value any)
		name      string
		recorded  string
		output    string
	}{
		{name: "the same output", normalise: nil, recorded: `{"id":9223372036854775807}`, output: `{"id": 9223372036854775807}`},
		{name: "a generated identifier normalised away", normalise: NormaliseFields(float64(0), true, "id"),
			recorded: `{"id":1180739088078503937,"name":"Gull"}`, output: `{"name":"Gull","id":1180739088078504000}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			goldenPath := filepath.Join(t.TempDir(), "golden", "output.json")
			assertGolden(t, goldenPath, []byte(test.recorded), &Suite{Normalise: test.normalise, Update: true})
			recorded, err := os.ReadFile(goldenPath)
			require.NoError(t, err)
			pretty, err := PrettyJSON([]byte(test.recorded))
			require.NoError(t, err)
			require.Equal(t, string(pretty), string(recorded))

			assertGolden(t, goldenPath, []byte(test.output), &Suite{Normalise: test.normalise, Update: false})
		})
	}
}
