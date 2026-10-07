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

package leakcheck

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestBuildOptionsPutTheSharedRulesBeforeAPackagesOwn(t *testing.T) {
	shared := 0
	if runtime.GOOS == "js" {
		shared = 1
	}
	testCases := []struct {
		name     string
		extra    []goleak.Option
		expected int
	}{
		{name: "no rules of the package's own", extra: nil, expected: shared},
		{name: "a rule of the package's own follows the shared ones", extra: []goleak.Option{IgnoreNetworkPollers()}, expected: shared + 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			options := buildOptions(testCase.extra)

			require.Len(t, baseOptions, shared, "only js/wasm shares a rule: its event handler's goroutine")
			require.Len(t, options, testCase.expected)
		})
	}
}

func TestFindLeaksAppliesAPackagesOwnIgnoreRules(t *testing.T) {
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		parkUntilReleased(started, release)
	}()
	<-started

	testCases := []struct {
		name  string
		extra []goleak.Option
	}{
		{name: "a goroutine the package's rule names is ignored", extra: []goleak.Option{goleak.IgnoreAnyFunction("seagull.sh/seagull/internal/leakcheck.parkUntilReleased")}},
		{name: "a goroutine parked in a function the rule names at the top is ignored", extra: []goleak.Option{goleak.IgnoreTopFunction("seagull.sh/seagull/internal/leakcheck.parkUntilReleased")}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.NoError(t, FindLeaks(testCase.extra...))
		})
	}

	close(release)
	<-done
	require.NoError(t, FindLeaks())
}

func parkUntilReleased(started chan<- struct{}, release <-chan struct{}) {
	close(started)
	<-release
}
