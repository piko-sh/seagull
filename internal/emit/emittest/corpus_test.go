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

package emittest

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCorpusNamesAreUnique(t *testing.T) {
	t.Parallel()

	cases := make(map[string]bool)
	for _, current := range corpusCases {
		require.False(t, cases[current.name], "two cases are named %s", current.name)
		cases[current.name] = true
		queries := make(map[string]bool)
		for _, file := range current.files {
			for _, entry := range file.queries {
				require.False(t, queries[entry.name], "case %s has two queries named %s", current.name, entry.name)
				queries[entry.name] = true
			}
		}
	}
}

func TestCorpusQueriesWriteEveryParameterTheyDeclare(t *testing.T) {
	t.Parallel()

	for _, current := range corpusCases {
		for _, file := range current.files {
			for _, entry := range file.queries {
				declared := make([]string, 0, len(entry.parameters))
				for _, parameter := range entry.parameters {
					declared = append(declared, parameter.name)
				}
				for _, variant := range entry.variants {
					written := firstAppearances(parameterOrder(variant, entry.parameters))
					require.ElementsMatch(t, declared, written, "%s/%s writes %v", current.name, entry.name, written)
					require.NotContains(t, strings.ReplaceAll(variant, "{n:T}", ""), "{"+"}", "%s/%s", current.name, entry.name)
				}
			}
		}
	}
}

func TestRuntimeCasesCallTheirQueries(t *testing.T) {
	t.Parallel()

	for _, current := range corpusCases {
		if !current.runtime {
			continue
		}
		called := calledQueries(current, nil)
		require.NotEmpty(t, called, "runtime case %s calls none of its queries", current.name)
	}
}

func TestByteCasesHoldTheirBytesInALiteral(t *testing.T) {
	t.Parallel()

	for _, class := range byteClasses {
		current, ok := findCase(byteCaseName(class))
		require.True(t, ok)
		require.Contains(t, current.files[0].queries[0].variants[0], "'"+class.content+"'")
	}
}

func TestFindCaseReportsAMissingCase(t *testing.T) {
	t.Parallel()

	_, ok := findCase("missing")
	require.False(t, ok)
	_, ok = findCase(caseCommands)
	require.True(t, ok)
	require.True(t, slices.Contains(generalCaseNames(), caseCommands))
	require.False(t, slices.Contains(generalCaseNames(), caseFolded))
}
