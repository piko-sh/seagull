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
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestUniqueNamesClaimsTheFirstFreeNumberedName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		names []string
		want  []string
	}{
		{name: "distinct names keep their names", names: []string{"id", "name"}, want: []string{"id", "name"}},
		{name: "a repeated name is numbered from two", names: []string{"id", "id", "id"}, want: []string{"id", "id2", "id3"}},
		{
			name:  "a numbered name already written is passed over",
			names: []string{"id", "id2", "id", "id"},
			want:  []string{"id", "id2", "id3", "id4"},
		},
		{
			name:  "a name written after its numbered form was claimed is numbered itself",
			names: []string{"id", "id", "id2", "id"},
			want:  []string{"id", "id2", "id22", "id3"},
		},
		{name: "an empty name is numbered like any other", names: []string{"", "", "2"}, want: []string{"", "2", "22"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, DisambiguateNames(testCase.names))
		})
	}
}

func TestDisambiguateNamesNamesAsTheFirstFreeNameSearchDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		seed  uint64
		count int
	}{
		{name: "a few names", seed: 1, count: 16},
		{name: "many names", seed: 2, count: 512},
		{name: "many more names", seed: 3, count: 2048},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			random := rand.New(rand.NewPCG(testCase.seed, testCase.seed))
			names := make([]string, testCase.count)
			for index := range names {
				names[index] = "x"
				if suffix := random.IntN(8); suffix > 1 {
					names[index] += strconv.Itoa(suffix)
				}
				if random.IntN(4) == 0 {
					names[index] += "2"
				}
			}

			require.Equal(t, goastutil.DisambiguateIdentifiers(names), DisambiguateNames(names))
		})
	}
}

func TestDisambiguateNamesTakesTimeLinearInTheNamesThatRepeat(t *testing.T) {
	t.Parallel()

	repeated := func(count int) []string {
		names := make([]string, count)
		for index := range names {
			names[index] = "x"
		}
		return names
	}
	smaller, larger := repeated(4_000), repeated(16_000)

	scaling.Linear(t, func() { DisambiguateNames(smaller) }, func() { DisambiguateNames(larger) })
}
