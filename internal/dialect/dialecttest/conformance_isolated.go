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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// deepNestingDepth is the parenthesis nesting of the deep-nesting check's query: far
	// beyond any realistic query, so only a depth guard can terminate it cleanly.
	deepNestingDepth = 100_000

	// isolatedCheckEnvironment names the environment variable that tells a test binary it is
	// the subprocess of an isolated check; its value is the subtest's full name.
	isolatedCheckEnvironment = "SEAGULL_DIALECTTEST_ISOLATED_CHECK"

	// isolatedCheckMaxStack caps the goroutine stack inside the subprocess, so a dialect
	// without a depth guard overflows quickly and cheaply instead of growing a stack of a
	// gigabyte first.
	isolatedCheckMaxStack = 64 << 20

	// isolatedCheckTimeout bounds how long the subprocess may run.
	isolatedCheckTimeout = 2 * time.Minute

	// isolatedOutputLimit is how many bytes of the subprocess's output a problem quotes.
	isolatedOutputLimit = 2048
)

// errIsolatedCheckTimeout is the cause recorded when the subprocess runs too long.
var errIsolatedCheckTimeout = errors.New("the isolated conformance check timed out")

// checkDeepNesting checks that deeply nested input terminates without a fatal error,
// either rejecting the query or yielding only an unknown-typed output. A missing guard
// can overflow the stack, which no recover can catch, so the check runs in a subprocess
// and a crash there fails only this check.
//
// Takes environment (checkEnvironment) which says whether this is the subprocess.
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkDeepNesting(ctx context.Context, environment checkEnvironment, fixture *Fixture) checkOutcome {
	if environment.inChild {
		previous := debug.SetMaxStack(isolatedCheckMaxStack)
		defer debug.SetMaxStack(previous)
		return deepNestingInProcess(ctx, fixture)
	}
	return runIsolated(ctx, environment)
}

// deepNestingInProcess runs the deep-nesting query and checks the result.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func deepNestingInProcess(ctx context.Context, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	deepQuery := directiveLine(engine, "Deep", "one") +
		"SELECT " + strings.Repeat("(", deepNestingDepth) + "1" + strings.Repeat(")", deepNestingDepth) + ";"
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"deep.sql": deepQuery})
	if err != nil {
		return failed("a bounded depth guard must terminate generation cleanly, not fail fatally: %v", err)
	}
	if result == nil {
		return failed("a generation result is expected even for guard-bounded input")
	}
	query := findQuery(emitter.queries, "Deep")
	if query == nil {
		return passed
	}
	if len(query.OutputColumns) > 1 {
		return failed("a guard-bounded nested expression must yield at most one output column, got %d",
			len(query.OutputColumns))
	}
	for index := range query.OutputColumns {
		if category := query.OutputColumns[index].SQLType.Category; category != sqlmodel.TypeCategoryUnknown {
			return failed("the depth guard truncates the nested expression, so its column must degrade to the "+
				"unknown category, got %s", category)
		}
	}
	return passed
}

// runIsolated reruns the current subtest in a subprocess of the test binary and turns its
// exit status into an outcome.
//
// Takes environment (checkEnvironment) which names the subtest.
//
// Returns checkOutcome which holds a problem when the subprocess failed, crashed or ran
// too long.
func runIsolated(ctx context.Context, environment checkEnvironment) checkOutcome {
	ctx, cancel := context.WithTimeoutCause(ctx, isolatedCheckTimeout, errIsolatedCheckTimeout)
	defer cancel()

	output, err := runTestInSubprocess(ctx, environment.testName)
	if cause := context.Cause(ctx); errors.Is(cause, errIsolatedCheckTimeout) {
		return failed("the isolated check's subprocess was stopped: %v (%v):\n%s", cause, err, outputTail(output))
	}
	if bytes.Contains(output, []byte("fatal error: stack overflow")) {
		return failed("deeply nested input overflowed the stack, which crashes the process; the dialect needs a "+
			"depth guard that stops parsing with an error:\n%s", outputHead(output))
	}
	if err != nil {
		return failed("the isolated check failed in its subprocess (%v):\n%s", err, outputTail(output))
	}
	if !bytes.Contains(output, []byte("--- PASS: "+environment.testName+" ")) {
		return failed("the isolated check's subprocess did not run %s:\n%s", environment.testName, outputTail(output))
	}
	return passed
}

// runTestInSubprocess reruns one test of the running test binary in a subprocess, naming
// the test in isolatedCheckEnvironment so it knows it is the subprocess.
//
// Takes testName (string) which is the test's full name.
//
// Returns []byte which is the subprocess's combined output.
// Returns error when the subprocess fails or cannot run.
func runTestInSubprocess(ctx context.Context, testName string) ([]byte, error) {
	//nolint:gosec // reruns the running test binary on a pattern built from its own subtest name
	command := exec.CommandContext(ctx, os.Args[0],
		"-test.run="+subtestPattern(testName),
		"-test.count=1",
		"-test.v",
	)
	command.Env = append(os.Environ(), isolatedCheckEnvironment+"="+testName)
	return command.CombinedOutput()
}

// subtestPattern builds a -test.run pattern that matches exactly one subtest.
//
// Takes testName (string) which is the subtest's full name.
//
// Returns string which anchors and quotes each level of the name.
func subtestPattern(testName string) string {
	levels := strings.Split(testName, "/")
	for index, level := range levels {
		levels[index] = "^" + regexp.QuoteMeta(level) + "$"
	}
	return strings.Join(levels, "/")
}

// outputHead returns the start of a subprocess's output, which names a crash before the
// goroutine dump that follows it, cut on a character boundary and followed by a note of
// how many bytes were left out.
//
// Takes output ([]byte) which is the combined output.
//
// Returns string which holds at most isolatedOutputLimit bytes of the output.
func outputHead(output []byte) string {
	if len(output) <= isolatedOutputLimit {
		return string(output)
	}
	cut := isolatedOutputLimit
	for step := 0; step < utf8.UTFMax && cut > 0 && !utf8.RuneStart(output[cut]); step++ {
		cut--
	}
	return string(output[:cut]) + fmt.Sprintf("\n... %d bytes omitted", len(output)-cut)
}

// outputTail returns the end of a subprocess's output, which holds a test failure, cut on
// a character boundary and preceded by a note of how many bytes were left out.
//
// Takes output ([]byte) which is the combined output.
//
// Returns string which holds at most isolatedOutputLimit bytes of the output.
func outputTail(output []byte) string {
	if len(output) <= isolatedOutputLimit {
		return string(output)
	}
	cut := len(output) - isolatedOutputLimit
	for step := 0; step < utf8.UTFMax && cut < len(output) && !utf8.RuneStart(output[cut]); step++ {
		cut++
	}
	return fmt.Sprintf("... %d bytes omitted\n", cut) + string(output[cut:])
}
