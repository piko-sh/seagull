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

package corpus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/cmd/internal/fuzztool/fuzztext"
)

const (
	// bundleTarget is the fuzz target whose input is a JSON file bundle.
	bundleTarget = "FuzzGenerateFiles"

	// bundleVersion is the only supported bundle envelope version.
	bundleVersion = "1"

	// maxBundleFiles is the most files a bundle directory may hold.
	maxBundleFiles = 8

	// filesKey and functionsKey name the bundle lists the reducer shortens.
	filesKey = "files"

	// functionsKey names the bundle's custom function list.
	functionsKey = "customFunctions"
)

var (
	// errEmptyModel refuses a structured model without its version byte.
	errEmptyModel = fmt.Errorf("%w: a structured model needs its version byte", errUnsupported)

	// errBundle marks every refusal of a file bundle.
	errBundle = fmt.Errorf("%w: unsupported source bundle", errUnsupported)

	// bundleDirectories are the directories a bundle file may sit in.
	bundleDirectories = []string{"migrations", "queries"}
)

// bundle is a decoded file envelope with the files it holds.
type bundle struct {
	// fields holds the whole envelope, kept for re-encoding.
	fields map[string]any

	// files holds each file entry in order.
	files []any
}

// decodeBundle validates a file envelope before any structural change.
//
// Takes data ([]byte) which is the JSON envelope.
//
// Returns bundle which holds the decoded envelope.
// Returns error when the envelope is malformed or holds unsafe paths.
func decodeBundle(data []byte) (bundle, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil || fields == nil || !atEnd(decoder) {
		return bundle{}, fmt.Errorf("%w: not a JSON object", errBundle)
	}
	version, isNumber := fields["version"].(json.Number)
	if !isNumber || version.String() != bundleVersion {
		return bundle{}, fmt.Errorf("%w: unsupported version", errBundle)
	}
	files, isList := fields[filesKey].([]any)
	if !isList {
		return bundle{}, fmt.Errorf("%w: missing bundle files", errBundle)
	}
	seen := make(map[string]bool, len(files))
	counts := make(map[string]int, len(bundleDirectories))
	for _, entry := range files {
		name, err := bundleFileName(entry)
		if err != nil {
			return bundle{}, err
		}
		directory, _, _ := strings.Cut(name, "/")
		if seen[name] {
			return bundle{}, fmt.Errorf("%w: unsafe or repeated bundle path", errBundle)
		}
		seen[name] = true
		counts[directory]++
		if counts[directory] > maxBundleFiles {
			return bundle{}, fmt.Errorf("%w: too many bundle files", errBundle)
		}
	}
	return bundle{fields: fields, files: files}, nil
}

// bundleFileName checks one bundle entry and returns its name.
//
// Takes entry (any) which is the decoded file entry.
//
// Returns string which is the entry's directory-qualified name.
// Returns error when the entry is malformed or its path is unsafe.
func bundleFileName(entry any) (string, error) {
	fields, isObject := entry.(map[string]any)
	if !isObject {
		return "", fmt.Errorf("%w: invalid bundle file", errBundle)
	}
	name, nameIsString := fields["name"].(string)
	_, sqlIsString := fields["sql"].(string)
	if !nameIsString || !sqlIsString {
		return "", fmt.Errorf("%w: invalid bundle file", errBundle)
	}
	parts := strings.Split(name, "/")
	unsafe := strings.ContainsAny(name, "\\\x00")
	if len(parts) != 2 || !slices.Contains(bundleDirectories, parts[0]) || !strings.HasSuffix(parts[1], ".sql") || unsafe {
		return "", fmt.Errorf("%w: unsafe or repeated bundle path", errBundle)
	}
	return name, nil
}

// encode writes the envelope with replaced fields as deterministic compact JSON.
//
// Takes replacements (map[string]any) which override envelope fields.
//
// Returns []byte which holds the JSON, or nil when it cannot be written.
func (decoded bundle) encode(replacements map[string]any) []byte {
	fields := maps.Clone(decoded.fields)
	maps.Copy(fields, replacements)
	encoded, err := fuzztext.MarshalASCII(fields, false)
	if err != nil {
		return nil
	}
	return encoded
}

// bundleCandidates normalises the envelope, drops files and custom functions, then
// reduces each file's SQL, always keeping every directory the envelope started with.
//
// Takes data ([]byte) which is a bundle that decodeBundle accepts.
//
// Returns iter.Seq[[]byte] which yields each candidate, or none for an invalid bundle.
func bundleCandidates(data []byte) iter.Seq[[]byte] {
	decoded, err := decodeBundle(data)
	if err != nil {
		return concat()
	}
	sqlReductions := make([]iter.Seq[[]byte], 0, len(decoded.files))
	for index := range decoded.files {
		sqlReductions = append(sqlReductions, fileSQLCandidates(decoded, index))
	}
	return concat(
		single(decoded.encode(nil)),
		fileCuts(decoded),
		functionCuts(decoded),
		concat(sqlReductions...),
	)
}

// fileCuts drops ranges of files that leave every directory with a file.
//
// Takes decoded (bundle) which is the envelope.
//
// Returns iter.Seq[[]byte] which yields each smaller envelope.
func fileCuts(decoded bundle) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		directories := fileDirectories(decoded.files)
		for start, end := range cuts(len(decoded.files), 0) {
			remaining := removeItems(decoded.files, start, end)
			if !maps.Equal(fileDirectories(remaining), directories) {
				continue
			}
			if !yield(decoded.encode(map[string]any{filesKey: remaining})) {
				return
			}
		}
	}
}

// functionCuts drops ranges of custom functions.
//
// Takes decoded (bundle) which is the envelope.
//
// Returns iter.Seq[[]byte] which yields each smaller envelope.
func functionCuts(decoded bundle) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		functions, isList := decoded.fields[functionsKey].([]any)
		if !isList {
			return
		}
		for start, end := range cuts(len(functions), 0) {
			if !yield(decoded.encode(map[string]any{functionsKey: removeItems(functions, start, end)})) {
				return
			}
		}
	}
}

// fileSQLCandidates yields the envelope with one file's SQL reduced, skipping
// reductions that split a character.
//
// Takes decoded (bundle) which is the envelope.
// Takes index (int) which is the file to reduce.
//
// Returns iter.Seq[[]byte] which yields each candidate envelope.
func fileSQLCandidates(decoded bundle, index int) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		entry, isObject := decoded.files[index].(map[string]any)
		if !isObject {
			return
		}
		sql, isString := entry["sql"].(string)
		if !isString {
			return
		}
		for candidate := range rawCandidates([]byte(sql)) {
			if !utf8.Valid(candidate) {
				continue
			}
			replacement := maps.Clone(entry)
			replacement["sql"] = string(candidate)
			files := slices.Clone(decoded.files)
			files[index] = replacement
			if !yield(decoded.encode(map[string]any{filesKey: files})) {
				return
			}
		}
	}
}

// fileDirectories returns the set of directories the files sit in.
//
// Takes files ([]any) which are validated file entries.
//
// Returns map[string]bool which holds each directory.
func fileDirectories(files []any) map[string]bool {
	directories := make(map[string]bool, len(bundleDirectories))
	for _, entry := range files {
		fields, isObject := entry.(map[string]any)
		if !isObject {
			continue
		}
		name, isString := fields["name"].(string)
		if !isString {
			continue
		}
		directory, _, _ := strings.Cut(name, "/")
		directories[directory] = true
	}
	return directories
}

// atEnd reports whether a decoder has nothing but whitespace left.
//
// Takes decoder (*json.Decoder) which has decoded one value.
//
// Returns bool which is true when no further token follows.
func atEnd(decoder *json.Decoder) bool {
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

// removeItems returns a new list without the items from start to end, never nil, so
// an emptied list is written as [] rather than null.
//
// Takes items ([]any) which is the original list.
// Takes start (int) and end (int) which bound the items to drop.
//
// Returns []any which is the shortened list.
func removeItems(items []any, start int, end int) []any {
	remaining := make([]any, 0, len(items)-(end-start))
	remaining = append(remaining, items[:start]...)
	return append(remaining, items[end:]...)
}
