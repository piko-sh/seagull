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

package testkit

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolchainEnvCapsProcs(t *testing.T) {
	t.Parallel()

	environment := ToolchainEnvironment()

	require.NotEmpty(t, environment)
	assert.Contains(t, environment, "GOMAXPROCS=2")
	assert.Len(t, environment, len(os.Environ())+1)
}

func TestToolchainEnvAppendsExtraAfterTheCap(t *testing.T) {
	t.Parallel()

	environment := ToolchainEnvironment("GOWORK=off", "CGO_ENABLED=0")

	capIndex := slices.Index(environment, "GOMAXPROCS=2")
	workIndex := slices.Index(environment, "GOWORK=off")
	cgoIndex := slices.Index(environment, "CGO_ENABLED=0")

	require.NotEqual(t, -1, capIndex)
	require.NotEqual(t, -1, workIndex)
	require.NotEqual(t, -1, cgoIndex)
	assert.Less(t, capIndex, workIndex)
	assert.Less(t, workIndex, cgoIndex)
}

func TestToolchainEnvLetsCallersOverrideTheCap(t *testing.T) {
	t.Parallel()

	environment := ToolchainEnvironment("GOMAXPROCS=8")

	last := slices.Index(environment, "GOMAXPROCS=8")
	require.NotEqual(t, -1, last)
	assert.Greater(t, last, slices.Index(environment, "GOMAXPROCS=2"))
}

func TestToolchainEnvDoesNotMutateTheProcessEnvironment(t *testing.T) {
	t.Parallel()

	before := os.Environ()
	_ = ToolchainEnvironment("GOWORK=off")

	assert.Equal(t, before, os.Environ())
}

func TestGoEnvironmentAddsTheCallersFlagsToTheDefaults(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		inherited   string
		environment []string
		expected    []string
	}{
		{
			name:        "the defaults alone turn the workspace and the version stamp off",
			inherited:   "",
			environment: nil,
			expected:    []string{"GOWORK=off", "GOFLAGS=-buildvcs=false"},
		},
		{
			name:        "inherited flags come first",
			inherited:   "-p=2  -modcacherw",
			environment: nil,
			expected:    []string{"GOWORK=off", "GOFLAGS=-p=2 -modcacherw -buildvcs=false"},
		},
		{
			name:        "the callers flags follow the defaults in one setting",
			inherited:   "-p=2",
			environment: []string{"GOFLAGS=-mod=mod", "CGO_ENABLED=0", "GOFLAGS=-trimpath"},
			expected:    []string{"GOWORK=off", "CGO_ENABLED=0", "GOFLAGS=-p=2 -buildvcs=false -mod=mod -trimpath"},
		},
		{
			name:        "a caller can turn the workspace back on",
			inherited:   "",
			environment: []string{"GOWORK=/work/go.work"},
			expected:    []string{"GOWORK=off", "GOWORK=/work/go.work", "GOFLAGS=-buildvcs=false"},
		},
		{
			name:        "a callers flag overrides a default",
			inherited:   "",
			environment: []string{"GOFLAGS=-buildvcs=true"},
			expected:    []string{"GOWORK=off", "GOFLAGS=-buildvcs=false -buildvcs=true"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, goEnvironment(testCase.inherited, testCase.environment))
		})
	}
}

func TestGoCommandBuildsACappedCommandInTheDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	testCases := []struct {
		name                string
		environment         []string
		arguments           []string
		expectedArguments   []string
		expectedEnvironment []string
	}{
		{
			name:                "a build carries the cap and the defaults",
			environment:         nil,
			arguments:           []string{"build", "-o", "lane", "."},
			expectedArguments:   []string{"go", "build", "-o", "lane", "."},
			expectedEnvironment: []string{"GOMAXPROCS=2", "GOWORK=off"},
		},
		{
			name:                "the callers settings follow the defaults",
			environment:         []string{"CGO_ENABLED=0"},
			arguments:           []string{"vet", "./..."},
			expectedArguments:   []string{"go", "vet", "./..."},
			expectedEnvironment: []string{"GOMAXPROCS=2", "GOWORK=off", "CGO_ENABLED=0"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			command := GoCommand(t, directory, testCase.environment, testCase.arguments...)

			assert.Equal(t, testCase.expectedArguments, command.Args)
			assert.Equal(t, directory, command.Dir)
			for _, setting := range testCase.expectedEnvironment {
				assert.Contains(t, command.Env, setting)
			}
			last := command.Env[len(command.Env)-1]
			assert.True(t, strings.HasPrefix(last, "GOFLAGS="), "GOFLAGS is the last setting, so it wins: %q", last)
			assert.Contains(t, strings.Fields(strings.TrimPrefix(last, "GOFLAGS=")), "-buildvcs=false")
		})
	}
}

func TestGoCommandContextDoesNotStartOnceItsContextEnds(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("the kit's deadline passed")
	cancel(cause)

	err := GoCommandContext(ctx, t.TempDir(), nil, "version").Run()

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestGoCommandRunsTheGoCommandWithTheDefaults(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		environment []string
		expected    []string
	}{
		{name: "the defaults reach the go command", environment: nil, expected: []string{"-buildvcs=false"}},
		{name: "the callers flags reach it too", environment: []string{"GOFLAGS=-mod=mod"}, expected: []string{"-buildvcs=false", "-mod=mod"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, err := GoCommand(t, t.TempDir(), testCase.environment, "env", "GOFLAGS", "GOWORK").Output()
			require.NoError(t, err)

			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			require.Len(t, lines, 2)
			flags := strings.Fields(lines[0])
			for _, flag := range testCase.expected {
				assert.Contains(t, flags, flag)
			}
			assert.Equal(t, "off", lines[1])
		})
	}
}
