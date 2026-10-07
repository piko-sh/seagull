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

package dialectkit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func TestFirstHookAnswerAsksTheMostRecentHookFirst(t *testing.T) {
	t.Parallel()

	answer := func(value int) func() *int { return func() *int { return &value } }
	decline := func() *int { return nil }
	cases := []struct {
		name     string
		hooks    []func() *int
		want     int
		answered bool
	}{
		{name: "no hooks", hooks: nil, want: 0, answered: false},
		{name: "every hook declines", hooks: []func() *int{decline, decline}, want: 0, answered: false},
		{name: "the most recent hook wins", hooks: []func() *int{answer(1), answer(2)}, want: 2, answered: true},
		{name: "a declining recent hook defers to an older one", hooks: []func() *int{answer(1), decline}, want: 1, answered: true},
		{name: "an answer of the zero value counts", hooks: []func() *int{answer(1), answer(0)}, want: 0, answered: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, answered := dialectkit.FirstHookAnswer(testCase.hooks, func(hook func() *int) *int { return hook() })

			assert.Equal(t, testCase.want, got)
			assert.Equal(t, testCase.answered, answered)
		})
	}
}

func TestFirstHookAnswerLetsAHookPanicGoOn(t *testing.T) {
	t.Parallel()

	hooks := []func() *int{func() *int { panic("hook bug") }}

	assert.PanicsWithValue(t, "hook bug", func() {
		dialectkit.FirstHookAnswer(hooks, func(hook func() *int) *int { return hook() })
	})
}
