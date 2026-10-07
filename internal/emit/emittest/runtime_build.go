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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"maps"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/testkit"
)

const (
	// recordedValueMark is the mark a harness writes before the number of a value whose
	// placeholder it leaves to the suite, as the pgx recorder does for a CopyFrom row.
	recordedValueMark = "\x00"

	// programPrelude declares what every runtime lane program shares: the record of a call,
	// the run of one call, and how an argument becomes text.
	programPrelude = `
type emittestOutcome struct {
	Package    string              ` + "`json:\"package\"`" + `
	Error      string              ` + "`json:\"error\"`" + `
	Panic      string              ` + "`json:\"panic\"`" + `
	Statements []emittestStatement ` + "`json:\"statements\"`" + `
	Call       int                 ` + "`json:\"call\"`" + `
	QueryNames bool                ` + "`json:\"queryNames\"`" + `
	NilSlice   bool                ` + "`json:\"nilSlice\"`" + `
}

var emittestNilSlice bool

type emittestStatement struct {
	SQL       string             ` + "`json:\"sql\"`" + `
	QueryName string             ` + "`json:\"queryName\"`" + `
	Arguments []emittestArgument ` + "`json:\"arguments\"`" + `
}

type emittestArgument struct {
	Name  string ` + "`json:\"name\"`" + `
	Text  string ` + "`json:\"text\"`" + `
	Type  string ` + "`json:\"type\"`" + `
	Bound string ` + "`json:\"bound\"`" + `
}

func emittestDiscard(...any) {}

func emittestRun(encoder *json.Encoder, packageName string, call int, names func(string) string, run func() error) {
	outcome := emittestOutcome{Package: packageName, Call: call, QueryNames: names != nil}
	emittestNilSlice = false
	Take()
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				outcome.Panic = fmt.Sprint(recovered)
			}
		}()
		if err := run(); err != nil {
			outcome.Error = err.Error()
		}
	}()
	for _, statement := range Take() {
		recorded := emittestStatement{SQL: statement.SQL}
		for _, argument := range statement.Arguments {
			recorded.Arguments = append(recorded.Arguments, emittestRender(argument))
		}
		if names != nil {
			recorded.QueryName = names(statement.SQL)
		}
		outcome.Statements = append(outcome.Statements, recorded)
	}
	outcome.NilSlice = emittestNilSlice
	_ = encoder.Encode(outcome)
}

func emittestRender(value any) (argument emittestArgument) {
	argument.Type = fmt.Sprintf("%T", value)
	if named, ok := value.(driver.NamedValue); ok {
		argument.Name, value = named.Name, named.Value
	}
	if named, ok := value.(sql.NamedArg); ok {
		argument.Name, value = named.Name, named.Value
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			argument.Text = fmt.Sprint("panic: ", recovered)
		}
	}()
	if valuer, ok := value.(driver.Valuer); ok {
		if converted, err := valuer.Value(); err == nil {
			value = converted
		}
	}
	argument.Bound = fmt.Sprintf("%T", value)
	argument.Text = emittestText(value)
	return argument
}

func emittestText(value any) string {
	reflected := reflect.ValueOf(value)
	for reflected.IsValid() && reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return "<nil>"
		}
		reflected = reflected.Elem()
	}
	if !reflected.IsValid() {
		return "<nil>"
	}
	if (reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Map) && reflected.IsNil() {
		return "<nil>"
	}
	if reflected.Kind() == reflect.Slice && reflected.Type().Elem().Kind() == reflect.Uint8 {
		return string(reflected.Bytes())
	}
	return fmt.Sprintf("%v", reflected.Interface())
}
`

	// programImports lists the imports the prelude needs.
	programImports = "context database/sql database/sql/driver encoding/json fmt os reflect"

	// goCommand is the go command, found on PATH.
	goCommand = "go"

	// laneBuildSettings are the environment settings the program is built with: offline,
	// with the toolchain at hand, without cgo, and free to complete the program's go.sum.
	laneBuildSettings = "GOFLAGS=-mod=mod GOPROXY=off GOTOOLCHAIN=local CGO_ENABLED=0"

	// mainFile is the program's main file.
	mainFile = "main.go"

	// maxBuildAttempts bounds how often the program is built: the first build, then one more
	// after each build failure that names packages to leave out.
	maxBuildAttempts = 2

	// buildFailurePrefix starts the failure of a package the compiler rejected.
	buildFailurePrefix = "the generated package does not build with the harness:\n"
)

// recordedValueMarks finds each value mark in recorded SQL: the mark and the value's
// number.
var recordedValueMarks = regexp.MustCompile(recordedValueMark + "([0-9]+)")

// build writes the program and builds it. A package that fails to build is left out with
// the compiler's report as its failure, and the rest are built again.
//
// Takes included ([]*lanePackage) which are the packages to build in.
//
// Returns string which is the program's path.
// Returns []*lanePackage which are the packages built in.
// Returns string which says why the program could not be built, or nothing when it was
// built or when every package carries its own failure.
func (lane *runtimeLane) build(ctx context.Context, included []*lanePackage) (string, []*lanePackage, string) {
	if _, err := exec.LookPath(goCommand); err != nil {
		return "", nil, "the go command is not on PATH: " + err.Error()
	}
	built, failure := buildLeavingOutFailures(included, func(packages []*lanePackage) (string, error) {
		if err := lane.writeProgram(ctx, packages); err != nil {
			return "", fmt.Errorf("writing the runtime lane program: %w", err)
		}
		return lane.buildBinary(ctx)
	})
	if len(built) == 0 {
		return "", nil, failure
	}
	return filepath.Join(lane.directory, laneBinary), built, ""
}

// writeProgram writes the program's module: its manifest, the harness, the harness
// modules, the generated packages and the main file.
//
// Takes included ([]*lanePackage) which are the packages to build in.
//
// Returns error when a file cannot be written, naming it.
func (lane *runtimeLane) writeProgram(ctx context.Context, included []*lanePackage) error {
	var manifest strings.Builder
	manifest.WriteString("module " + laneModule + "\n\ngo " + goVersion() + "\n")
	for index, modulePath := range slices.Sorted(maps.Keys(lane.harness.Modules)) {
		directory := numberedName("modules/m", index)
		if err := lane.writeModule(directory, modulePath, lane.harness.Modules[modulePath]); err != nil {
			return err
		}
		manifest.WriteString("\nrequire " + modulePath + " " + stubVersion(modulePath) + "\n\nreplace " + modulePath + " => ./" + directory + "\n")
	}
	if err := lane.writeFile("go.mod", []byte(manifest.String())); err != nil {
		return err
	}
	if err := lane.writeFile("harness.go", []byte(lane.harness.mainSource())); err != nil {
		return err
	}
	for _, entry := range included {
		for _, file := range entry.subject.generation(ctx, entry.caseName).generated.Files {
			if err := lane.writeFile(path.Join(entry.name, file.Name), file.Content); err != nil {
				return err
			}
		}
	}
	source, err := programSource(included)
	if err != nil {
		return fmt.Errorf("formatting %s: %w", mainFile, err)
	}
	return lane.writeFile(mainFile, source)
}

// execute runs the built program and gives each call its outcome.
//
// Takes binary (string) which is the program's path.
// Takes included ([]*lanePackage) which are the packages built in.
//
// Returns string which says why the program failed, or nothing.
func (lane *runtimeLane) execute(ctx context.Context, binary string, included []*lanePackage) string {
	command := exec.CommandContext(ctx, binary)
	command.Dir = lane.directory
	var standardOutput, standardError bytes.Buffer
	command.Stdout, command.Stderr = &standardOutput, &standardError
	runError := command.Run()
	decoder := json.NewDecoder(&standardOutput)
	for {
		var outcome laneOutcome
		if err := decoder.Decode(&outcome); err != nil {
			if !errors.Is(err, io.EOF) {
				return "reading the runtime lane program's output: " + err.Error()
			}
			break
		}
		index := slices.IndexFunc(included, func(entry *lanePackage) bool { return entry.name == outcome.Package })
		if index >= 0 && outcome.Call >= 0 && outcome.Call < len(included[index].calls) {
			writeMarkedPlaceholders(&outcome, &included[index].subject.info)
			included[index].calls[outcome.Call].outcome = &outcome
		}
	}
	if runError != nil {
		return "the runtime lane program failed: " + runError.Error() + "\n" + standardError.String()
	}
	return ""
}

// buildBinary builds the program with the go command in its directory, offline and with
// the standard library only besides the harness modules.
//
// Returns string which is the command's combined output.
// Returns error when the build fails.
func (lane *runtimeLane) buildBinary(ctx context.Context) (string, error) {
	command := testkit.GoCommandContext(ctx, lane.directory, strings.Fields(laneBuildSettings), "build", "-o", laneBinary, ".")
	output, err := command.CombinedOutput()
	return string(output), err
}

// excludeFailed marks each package a build failure names as failed, with the compiler's
// report: the lines after the package's "# path" line, up to the next.
//
// Takes included ([]*lanePackage) which are the packages built.
// Takes output (string) which is the build's output.
//
// Returns []*lanePackage which holds the packages marked.
func excludeFailed(included []*lanePackage, output string) []*lanePackage {
	var failed []*lanePackage
	var current *lanePackage
	for line := range strings.Lines(output) {
		if importPath, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
			current = nil
			index := slices.IndexFunc(included, func(entry *lanePackage) bool { return importPath == laneModule+"/"+entry.name })
			if index >= 0 {
				current = included[index]
				current.failure = buildFailurePrefix
				failed = append(failed, current)
			}
			continue
		}
		if current != nil {
			current.failure += line
		}
	}
	return failed
}

// programSource writes the program's main file: main calls each package's function in
// turn, and each function calls its package's methods.
//
// Takes included ([]*lanePackage) which are the packages to call.
//
// Returns []byte which is the formatted source.
// Returns error when the source does not format.
func programSource(included []*lanePackage) ([]byte, error) {
	imports := strings.Fields(programImports)
	aliased := make(map[string]bool)
	for _, entry := range included {
		for importPath := range entry.imports {
			aliased[importPath] = true
		}
	}
	var source strings.Builder
	source.WriteString("package main\n\nimport (\n")
	for _, importPath := range imports {
		source.WriteString(strconv.Quote(importPath) + "\n")
	}
	for _, importPath := range slices.Sorted(maps.Keys(aliased)) {
		source.WriteString(importAliasOf(importPath) + " " + strconv.Quote(importPath) + "\n")
	}
	for _, entry := range included {
		source.WriteString(entry.name + " " + strconv.Quote(laneModule+"/"+entry.name) + "\n")
	}
	source.WriteString(")\n" + programPrelude + "\nfunc main() {\nctx := context.Background()\nencoder := json.NewEncoder(os.Stdout)\n")
	for _, entry := range included {
		source.WriteString("emittestPackage" + entry.name + "(ctx, encoder)\n")
	}
	source.WriteString("}\n")
	for _, entry := range included {
		source.WriteString("\nfunc emittestPackage" + entry.name + "(ctx context.Context, encoder *json.Encoder) {\n" + entry.source + "}\n")
	}
	return format.Source([]byte(source.String()))
}

// stubVersion returns the version the program requires a harness module at, which a
// module path ending in a major version element must match.
//
// Takes modulePath (string) which is the module's path.
//
// Returns string which is the version, such as v0.0.0 or v2.0.0.
func stubVersion(modulePath string) string {
	last := path.Base(modulePath)
	if versionSuffix.MatchString(last) && last != "v0" && last != "v1" {
		return last + ".0.0"
	}
	return "v0.0.0"
}

// writeMarkedPlaceholders writes, in place of each value mark in the SQL of an outcome's
// statements, the placeholder the dialect binds that value with by position.
//
// A harness marks the values of a row it records without SQL of its own, such as the pgx
// recorder's CopyFrom row, since it cannot know the dialect's form.
//
// Takes outcome (*laneOutcome) which holds the statements, rewritten in place.
// Takes info (*dialect.Info) which describes the dialect the package was generated with.
func writeMarkedPlaceholders(outcome *laneOutcome, info *dialect.Info) {
	form := info.Placeholders.PositionalForm()
	for index := range outcome.Statements {
		statement := &outcome.Statements[index]
		statement.SQL = recordedValueMarks.ReplaceAllStringFunc(statement.SQL, func(mark string) string {
			number, err := strconv.Atoi(mark[len(recordedValueMark):])
			if err != nil {
				return mark
			}
			return form.Render(number, "", "")
		})
	}
}

// buildLeavingOutFailures builds until a build succeeds, up to maxBuildAttempts builds,
// leaving out after each failed build the packages the compiler names.
//
// Takes included ([]*lanePackage) which are the packages to build in.
// Takes attempt (func([]*lanePackage) (string, error)) which writes and builds the
// program with the packages given, and returns the compiler's output.
//
// Returns []*lanePackage which are the packages built in, or nil when no build succeeded.
// Returns string which says why the program could not be built, or nothing when every
// package was left out with a failure of its own.
func buildLeavingOutFailures(included []*lanePackage, attempt func([]*lanePackage) (string, error)) ([]*lanePackage, string) {
	leftOut := 0
	output := ""
	for range maxBuildAttempts {
		var err error
		output, err = attempt(included)
		if err == nil {
			return included, ""
		}
		failed := excludeFailed(included, output)
		if len(failed) == 0 {
			return nil, "building the runtime lane program failed: " + err.Error() + "\n" + output
		}
		leftOut += len(failed)
		included = slices.DeleteFunc(included, func(entry *lanePackage) bool { return slices.Contains(failed, entry) })
		if len(included) == 0 {
			return nil, ""
		}
	}
	return nil, fmt.Sprintf("building the runtime lane program still failed after leaving out %d packages that do not build with the harness:\n%s",
		leftOut, output)
}
