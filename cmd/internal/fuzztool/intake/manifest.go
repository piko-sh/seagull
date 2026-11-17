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

package intake

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
)

const (
	// maxManifestBytes bounds the suite manifest.
	maxManifestBytes = 1 << 20

	// formatLogic is the sqllogictest file format.
	formatLogic = "sqllogictest"

	// formatSQL is the plain SQL file format.
	formatSQL = "sql"
)

var (
	// requiredFields are the manifest fields every suite must give, in record order.
	requiredFields = []string{"name", "licence", "revision", "upstream", "format", "dialects"}

	// suiteName is the shape of a suite name, which becomes a directory name.
	suiteName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

	// ErrManifest marks every refusal of a suite manifest.
	ErrManifest = errors.New("invalid intake manifest")
)

// Manifest describes an external suite.
type Manifest struct {
	// fields holds the decoded manifest.
	fields map[string]any

	// aliases maps each dialect to the engine names its conditions use.
	aliases map[string][]string

	// Name is the suite's directory name.
	Name string

	// Format is formatLogic or formatSQL.
	Format string

	// Dialects are the dialects the suite's SQL is written for.
	Dialects []string
}

// ReadManifest reads and checks a suite manifest.
//
// Takes path (string) which is the manifest file.
//
// Returns Manifest which describes the suite.
// Returns error which wraps ErrManifest when the manifest is unreadable or incomplete.
func ReadManifest(path string) (Manifest, error) {
	fields, err := readObject(path)
	if err != nil {
		return Manifest{}, err
	}
	name, isString := fields["name"].(string)
	if !isString || !suiteName.MatchString(name) {
		return Manifest{}, fmt.Errorf("%w: the manifest name must be lower-case letters, digits, - or _", ErrManifest)
	}
	var missing []string
	for _, field := range requiredFields {
		if !present(fields[field]) {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return Manifest{}, fmt.Errorf("%w: the manifest lacks %s", ErrManifest, strings.Join(missing, ", "))
	}
	format, isString := fields["format"].(string)
	if !isString || format != formatLogic && format != formatSQL {
		return Manifest{}, fmt.Errorf("%w: the format must be sqllogictest or sql", ErrManifest)
	}
	dialects, isList := stringList(fields["dialects"])
	if !isList {
		return Manifest{}, fmt.Errorf("%w: the dialects must be a list of names", ErrManifest)
	}
	aliases, err := readAliases(fields["aliases"])
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{fields: fields, aliases: aliases, Name: name, Format: format, Dialects: dialects}, nil
}

// readObject reads a bounded JSON object.
//
// Takes path (string) which is the file to read.
//
// Returns map[string]any which holds the object.
// Returns error which wraps ErrManifest when the file is not a readable JSON object.
func readObject(path string) (map[string]any, error) {
	file, err := os.Open(path) //nolint:gosec // The caller names the manifest to read.
	if err != nil {
		return nil, fmt.Errorf("%w: the intake manifest is not readable JSON: %w", ErrManifest, err)
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil || len(content) > maxManifestBytes {
		return nil, fmt.Errorf("%w: the intake manifest is not readable JSON", ErrManifest)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: the intake manifest is not readable JSON: %w", ErrManifest, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: the intake manifest is not readable JSON", ErrManifest)
	}
	fields, isObject := value.(map[string]any)
	if !isObject {
		return nil, fmt.Errorf("%w: the manifest name must be lower-case letters, digits, - or _", ErrManifest)
	}
	return fields, nil
}

// readAliases checks the optional alias map.
//
// Takes value (any) which is the decoded aliases field.
//
// Returns map[string][]string which maps each dialect to its engine names.
// Returns error which wraps ErrManifest when the aliases are malformed.
func readAliases(value any) (map[string][]string, error) {
	aliases := make(map[string][]string)
	if value == nil {
		return aliases, nil
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		return nil, fmt.Errorf("%w: the aliases must map each dialect to a list of names", ErrManifest)
	}
	for dialect, names := range object {
		list, isList := stringList(names)
		if !isList {
			return nil, fmt.Errorf("%w: the aliases must map each dialect to a list of names", ErrManifest)
		}
		aliases[dialect] = list
	}
	return aliases, nil
}

// stringList converts a decoded JSON list of strings.
//
// Takes value (any) which is the decoded value.
//
// Returns []string which holds the strings.
// Returns bool which is false when the value is not a list of strings.
func stringList(value any) ([]string, bool) {
	items, isList := value.([]any)
	if !isList {
		return nil, false
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		text, isString := item.(string)
		if !isString {
			return nil, false
		}
		texts = append(texts, text)
	}
	return texts, true
}

// present reports whether a manifest value is given and not empty.
//
// Takes value (any) which is the decoded value.
//
// Returns bool which is false for a missing, null, false, zero or empty value.
func present(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case json.Number:
		number, err := typed.Float64()
		return err != nil || number != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

// applies returns the dialects a record applies to under its conditions.
//
// Takes dialects ([]string) which are the suite's dialects.
// Takes conditions ([]condition) which are the record's onlyif and skipif lines.
// Takes aliases (map[string][]string) which maps dialects to engine names.
//
// Returns []string which holds the chosen dialects in suite order.
func applies(dialects []string, conditions []condition, aliases map[string][]string) []string {
	chosen := slices.Clone(dialects)
	for _, rule := range conditions {
		chosen = slices.DeleteFunc(chosen, func(dialect string) bool {
			names, found := aliases[dialect]
			if !found {
				names = []string{dialect}
			}
			return slices.Contains(names, rule.engine) != rule.only
		})
	}
	return chosen
}
