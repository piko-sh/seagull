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

package clickhouse

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

const (
	// aliasLineFields is how many words a line of the alias table holds: the alias, then the
	// function it names.
	aliasLineFields = 2
)

var (
	// functionAliasSource is the ClickHouse alias table, one alias and the function it names
	// per line.
	//
	//go:embed builtin_function_aliases.spec
	functionAliasSource string

	// functionAliasTable decodes functionAliasSource the first time it is read: the aliases
	// of every line that decoded, and an error naming each line that did not, which every
	// function catalogue New builds reports.
	functionAliasTable = sync.OnceValues(func() (map[string]string, error) {
		aliases, err := parseFunctionAliases(functionAliasSource)
		if err != nil {
			return aliases, fmt.Errorf("decoding embedded function alias table: %w", err)
		}
		return aliases, nil
	})
)

// registerFunctionAliases files each alias of an alias table under its name with the
// signatures of the function it names, when the catalogue holds that function and does
// not hold the alias already. The aliases of the lines that decoded are filed whatever
// the table's error.
//
// Takes table (func() (map[string]string, error)) which decodes the alias table.
//
// Returns error which names each line of the table that did not decode, or nil.
func (builder *FunctionCatalogueBuilder) registerFunctionAliases(table func() (map[string]string, error)) error {
	aliases, err := table()
	functions := builder.Catalogue.Functions
	for alias, target := range aliases {
		if _, registered := functions[alias]; registered {
			continue
		}
		if signatures, found := functions[target]; found {
			functions[alias] = slices.Clone(signatures)
		}
	}
	return err
}

// functionAliases returns the aliases of the lines of the alias table that decoded, each
// lower-cased alias mapped to the lower-cased name of the function it names.
//
// The error naming a line that did not decode is reported once, where a function
// catalogue files the aliases, and New keeps it for every ApplyDDL and AnalyseQuery to
// return.
//
// Returns map[string]string which maps each alias to its function.
func functionAliases() map[string]string {
	aliases, _ := functionAliasTable()
	return aliases
}

// parseFunctionAliases decodes an alias table: one alias and the function it names per
// line, separated by spaces, with blank lines and lines starting with # ignored. An alias
// may be listed again in another case, as ClickHouse lists DATE_DIFF and date_diff, when
// it names the same function.
//
// Takes source (string) which is the table's text.
//
// Returns map[string]string which maps each lower-cased alias to the lower-cased name of
// its function, for every line that decoded.
// Returns error which names each line that does not hold exactly two names, each alias
// listed again for another function, and a failure reading the text, or nil when every
// line decoded.
func parseFunctionAliases(source string) (map[string]string, error) {
	aliases := make(map[string]string)
	var problems []error
	scanner := bufio.NewScanner(strings.NewReader(source))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != aliasLineFields {
			problems = append(problems, fmt.Errorf("function alias table line %d: %w, got %d words", lineNumber, errMalformedAliasLine, len(fields)))
			continue
		}
		alias, target := strings.ToLower(fields[0]), strings.ToLower(fields[1])
		if listed, isListed := aliases[alias]; isListed && listed != target {
			problems = append(problems, fmt.Errorf("function alias table line %d: %q: %w", lineNumber, fields[0], errConflictingAlias))
			continue
		}
		aliases[alias] = target
	}
	if err := scanner.Err(); err != nil {
		problems = append(problems, fmt.Errorf("function alias table after line %d: %w", lineNumber, err))
	}
	return aliases, errors.Join(problems...)
}

// canonicalFunctionName returns the function a name calls: the function an alias names,
// or the name itself.
//
// Takes name (string) which is the name a call uses.
//
// Returns string which is the lower-cased name of the function the call reaches.
func canonicalFunctionName(name string) string {
	lowered := strings.ToLower(name)
	if target, isAlias := functionAliases()[lowered]; isAlias {
		return target
	}
	return lowered
}
