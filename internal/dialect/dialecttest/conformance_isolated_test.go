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

package dialecttest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	unguardedProbeTest   = "TestUnguardedDeepNestingProbe"
	stalledProbeTest     = "TestStalledIsolatedProbe"
	stalledProbeDeadline = 500 * time.Millisecond
)

type unguardedDialect struct {
	*postgres.Dialect
}

func (unguardedDialect) AnalyseQuery(_ context.Context, _ *sqlmodel.Catalogue, _ ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
	return nil, descendWithoutBound(0)
}

func unguardedFixture() *Fixture {
	return &Fixture{
		NewDialect: func() dialect.DialectPort {
			return unguardedDialect{Dialect: postgres.New()}
		},
		BaseMigration: "CREATE TABLE items (id integer PRIMARY KEY, name text);",
	}
}

func descendWithoutBound(depth int) error {
	var frame [512]byte
	frame[depth%len(frame)] = byte(depth)
	if depth < 0 {
		return nil
	}
	if err := descendWithoutBound(depth + 1); err != nil {
		return fmt.Errorf("depth %d holding %d: %w", depth, frame[depth%len(frame)], err)
	}
	return nil
}

func TestUnguardedDeepNestingProbe(t *testing.T) {
	if os.Getenv(isolatedCheckEnvironment) != t.Name() {
		t.Skip("runs only as the subprocess of TestIsolatedCheckReportsStackOverflow")
	}
	fixture := unguardedFixture()
	runConformanceCheck(t, conformanceCheck{name: "DeepNestingTerminates", run: checkDeepNesting}, fixture, knownFailureTable(fixture, conformanceChecks))
}

func TestIsolatedCheckReportsStackOverflow(t *testing.T) {
	if testing.Short() {
		t.Skip("deliberately overflows the stack of a subprocess; skipped under -short")
	}
	t.Parallel()

	outcome := checkDeepNesting(t.Context(), checkEnvironment{testName: unguardedProbeTest, inChild: false}, unguardedFixture())
	require.Len(t, outcome.Problems, 1)
	require.Contains(t, outcome.Problems[0], "overflowed the stack")
}

func TestIsolatedCheckPassesWithAGuard(t *testing.T) {
	t.Parallel()

	fixture := unguardedFixture()
	fixture.NewDialect = func() dialect.DialectPort { return postgres.New() }
	outcome := checkDeepNesting(t.Context(), checkEnvironment{testName: "TestIsolatedCheckPassesWithAGuard", inChild: true}, fixture)
	require.Empty(t, outcome.Problems)
}

func TestSubtestPattern(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		expected string
	}{
		{name: "TestConformance", expected: "^TestConformance$"},
		{name: "TestConformance/postgres/DeepNestingTerminates", expected: "^TestConformance$/^postgres$/^DeepNestingTerminates$"},
		{name: "TestA/b.c+d", expected: `^TestA$/^b\.c\+d$`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, subtestPattern(testCase.name))
		})
	}
}

func TestStalledIsolatedProbe(t *testing.T) {
	if os.Getenv(isolatedCheckEnvironment) != t.Name() {
		t.Skip("runs only as the subprocess of TestIsolatedCheckReportsItsTimeout")
	}
	<-t.Context().Done()
}

func TestIsolatedCheckReportsItsTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeoutCause(t.Context(), stalledProbeDeadline, errIsolatedCheckTimeout)
	defer cancel()
	outcome := runIsolated(ctx, checkEnvironment{testName: stalledProbeTest, inChild: false})
	require.Len(t, outcome.Problems, 1)
	require.Contains(t, outcome.Problems[0], "the isolated check's subprocess was stopped: "+errIsolatedCheckTimeout.Error())
}

func TestOutputHeadAndTailCutOnCharacterBoundaries(t *testing.T) {
	t.Parallel()

	fits := strings.Repeat("a", isolatedOutputLimit)
	straddling := strings.Repeat("a", isolatedOutputLimit-1) + "é" + strings.Repeat("b", 10)
	testCases := []struct {
		name         string
		output       string
		expectedHead string
		expectedTail string
	}{
		{
			name:         "output within the limit is kept whole",
			output:       fits,
			expectedHead: fits,
			expectedTail: fits,
		},
		{
			name:         "ASCII output over the limit is cut at the limit",
			output:       fits + "xyz",
			expectedHead: fits + "\n... 3 bytes omitted",
			expectedTail: "... 3 bytes omitted\n" + strings.Repeat("a", isolatedOutputLimit-3) + "xyz",
		},
		{
			name:         "a character across the head's limit is left out whole",
			output:       straddling,
			expectedHead: strings.Repeat("a", isolatedOutputLimit-1) + "\n... 12 bytes omitted",
			expectedTail: "... 11 bytes omitted\n" + strings.Repeat("a", isolatedOutputLimit-12) + "é" + strings.Repeat("b", 10),
		},
		{
			name:         "a character across the tail's limit is left out whole",
			output:       "é" + strings.Repeat("c", isolatedOutputLimit-1),
			expectedHead: "é" + strings.Repeat("c", isolatedOutputLimit-2) + "\n... 1 bytes omitted",
			expectedTail: "... 2 bytes omitted\n" + strings.Repeat("c", isolatedOutputLimit-1),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			head := outputHead([]byte(testCase.output))
			tail := outputTail([]byte(testCase.output))
			require.Equal(t, testCase.expectedHead, head)
			require.Equal(t, testCase.expectedTail, tail)
			require.True(t, utf8.ValidString(head))
			require.True(t, utf8.ValidString(tail))
		})
	}
}
