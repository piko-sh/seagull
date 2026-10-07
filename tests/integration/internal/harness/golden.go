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
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// PrettyJSON indents a JSON document two spaces per level and ends it with a newline, the
// form a golden file is recorded in. Each number keeps the digits it was written with.
//
// Takes document ([]byte) which is the JSON document.
//
// Returns []byte which is the indented document.
// Returns error when the document is not one valid JSON value.
func PrettyJSON(document []byte) ([]byte, error) {
	parsed, err := decodeKeepingNumbers(document)
	if err != nil {
		return nil, err
	}
	return encodePretty(parsed)
}

// Normalised decodes a JSON document, passes it through normalise and encodes it again in
// the golden form, with each number keeping the digits it was written with, so two
// documents compare equal byte for byte exactly when they hold the same values.
//
// Takes document ([]byte) which is the JSON document.
// Takes normalise (func(value any)) which rewrites the decoded value in place, or is nil
// to leave it as it is. Numbers reach it as json.Number.
//
// Returns []byte which is the normalised document.
// Returns error when the document is not one valid JSON value.
func Normalised(document []byte, normalise func(value any)) ([]byte, error) {
	parsed, err := decodeKeepingNumbers(document)
	if err != nil {
		return nil, err
	}
	if normalise != nil {
		normalise(parsed)
	}
	return encodePretty(parsed)
}

// NormaliseFields returns a Normalise that replaces the value of every object member with
// one of the given names, at any depth, with replacement, so identifiers and names a
// database generates compare equal from run to run. Only values of the replacement's kind
// are replaced when onlySameKind is true, which keeps, say, a string id intact while
// generated numeric ids are replaced by zero.
//
// Takes replacement (any) which is the value written in place of each match.
// Takes onlySameKind (bool) which limits the replacement to values of the same JSON kind
// as replacement.
// Takes names (...string) which are the member names to replace.
//
// Returns func(value any) which normalises a decoded document in place.
func NormaliseFields(replacement any, onlySameKind bool, names ...string) func(value any) {
	matches := make(map[string]bool, len(names))
	for _, name := range names {
		matches[name] = true
	}
	var normalise func(value any)
	normalise = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if matches[key] && (!onlySameKind || sameKind(child, replacement)) {
					typed[key] = replacement
					continue
				}
				normalise(child)
			}
		case []any:
			for _, child := range typed {
				normalise(child)
			}
		}
	}
	return normalise
}

// assertGolden compares a runner's JSON output with a case's golden file, or records it
// there when the suite is updating its goldens. Both sides pass through the suite's
// Normalise before they are compared, so values a run cannot reproduce compare equal.
// Numbers keep the digits they were written with, so a 64-bit integer that differs from
// the golden only past what a float64 holds still fails the comparison.
//
// Takes goldenPath (string) which is the golden file's path.
// Takes output ([]byte) which is the runner's output.
// Takes suite (*Suite) which says whether to update and how to normalise.
func assertGolden(t *testing.T, goldenPath string, output []byte, suite *Suite) {
	t.Helper()

	pretty, err := PrettyJSON(output)
	require.NoError(t, err, "the runner printed invalid JSON:\n%s", output)
	if suite.Update {
		writeFile(t, goldenPath, pretty)
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	expected, err := fs.ReadFile(os.DirFS(filepath.Dir(goldenPath)), filepath.Base(goldenPath))
	if errors.Is(err, fs.ErrNotExist) {
		require.FailNowf(t, "golden file not found", "%s (run with -update to record it)", goldenPath)
	}
	require.NoError(t, err)

	normalisedExpected, err := Normalised(expected, suite.Normalise)
	require.NoError(t, err, "the golden file %s holds invalid JSON", goldenPath)
	normalisedActual, err := Normalised(pretty, suite.Normalise)
	require.NoError(t, err)
	require.Equal(t, string(normalisedExpected), string(normalisedActual), "golden file mismatch: %s", goldenPath)
}

// decodeKeepingNumbers decodes the one JSON value a document holds, each number as the
// json.Number of the digits it was written with.
//
// Takes document ([]byte) which is the JSON document.
//
// Returns any which is the decoded value.
// Returns error when the document is not valid JSON or holds more than one value.
func decodeKeepingNumbers(document []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	var parsed any
	if err := decodeOnly(decoder, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// decodeOnly decodes the one JSON value a decoder reads into target and fails when
// anything but white space follows it.
//
// Takes decoder (*json.Decoder) which reads the document.
// Takes target (any) which receives the value.
//
// Returns error when the value does not decode or more data follows it.
func decodeOnly(decoder *json.Decoder, target any) error {
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errTrailingData
	}
	return nil
}

// encodePretty encodes a decoded value two spaces per level, ending with a newline.
//
// Takes value (any) which is the decoded value.
//
// Returns []byte which is the encoded document.
// Returns error when the value cannot be encoded.
func encodePretty(value any) ([]byte, error) {
	pretty, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(pretty, '\n'), nil
}

// sameKind reports whether two decoded JSON values are of the same kind. A json.Number
// and a Go number are both numbers.
//
// Takes left (any) which is one value.
// Takes right (any) which is the other.
//
// Returns bool which is true when both are numbers, both strings, both booleans, both
// objects, both arrays or both null.
func sameKind(left, right any) bool {
	switch left.(type) {
	case json.Number, float64, int, int64:
		return isNumber(right)
	case string:
		_, ok := right.(string)
		return ok
	case bool:
		_, ok := right.(bool)
		return ok
	case map[string]any:
		_, ok := right.(map[string]any)
		return ok
	case []any:
		_, ok := right.([]any)
		return ok
	default:
		return left == nil && right == nil
	}
}

// isNumber reports whether a value is a JSON number as a decoder or a caller writes one.
//
// Takes value (any) which is the value.
//
// Returns bool which is true for a json.Number, float64, int or int64.
func isNumber(value any) bool {
	switch value.(type) {
	case json.Number, float64, int, int64:
		return true
	default:
		return false
	}
}
