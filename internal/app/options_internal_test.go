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

package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func TestPipelineConfigCopiesTheOverridesAndFunctionsDeeply(t *testing.T) {
	t.Parallel()

	nonNull := true
	override := TypeOverride{Column: "tasks.title", GoName: "string", StructTags: map[string]string{"db": "title"}, NonNullElements: &nonNull}
	function := CustomFunction{Name: "slugify", ReturnType: "text", Arguments: []string{"text"}}
	collected := newSettings()
	WithTypeOverrides(override)(collected)
	WithCustomFunctions(function)(collected)

	config, err := collected.pipelineConfig(sqlite.New())
	require.NoError(t, err)
	require.Len(t, config.TypeOverrides, 1)
	require.Len(t, config.CustomFunctions, 1)

	override.StructTags["json"] = "title"
	nonNull = false
	function.Arguments[0] = "integer"

	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "struct tags", got: config.TypeOverrides[0].StructTags, want: map[string]string{"db": "title"}},
		{name: "element choice", got: *config.TypeOverrides[0].NonNullElements, want: true},
		{name: "arguments", got: config.CustomFunctions[0].Arguments, want: []string{"text"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.got)
		})
	}
}

func TestCloneHelpersKeepNilAsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, cloneTypeOverrides(nil))
	assert.Nil(t, cloneCustomFunctions(nil))
	assert.Nil(t, cloneTypeOverrides([]TypeOverride{{SQLTypeName: "text", GoName: "string"}})[0].NonNullElements)
}

func TestPipelineConfigPicksTheDefaults(t *testing.T) {
	t.Parallel()

	config, err := newSettings().pipelineConfig(sqlite.New())
	require.NoError(t, err)

	assert.IsType(t, &databasesql.Emitter{}, config.Emitter)
	assert.Equal(t, observe.SystemClock(), config.Clock)
	assert.Equal(t, defaultRuntimeTables, config.RuntimeTables)
	assert.Nil(t, config.Observer)
}

func TestPipelineConfigKeepsTheChosenEmitterAndClock(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{}
	clock := fixedClock{now: time.Unix(0, 0)}
	collected := newSettings()
	WithEmitter(emitter)(collected)
	WithClock(clock)(collected)

	config, err := collected.pipelineConfig(sqlite.New())
	require.NoError(t, err)

	assert.Same(t, emitter, config.Emitter)
	assert.Equal(t, clock, config.Clock)
}
