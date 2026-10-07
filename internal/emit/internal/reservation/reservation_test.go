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

package reservation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/emit"
	emitclickhouse "seagull.sh/seagull/internal/emit/clickhouse"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/reservation"
	"seagull.sh/seagull/internal/emit/pgx"
	"seagull.sh/seagull/internal/sqlmodel"
)

type builtinEmitter interface {
	emit.EmitterPort
	emit.ReserverPort
	emit.CheckerPort
	emit.TypeDefaulterPort
}

type recordingEmitter struct {
	builtinEmitter
	request *emit.Request
}

func (recorder *recordingEmitter) Emit(ctx context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	recorder.request = new(*request)
	return recorder.builtinEmitter.Emit(ctx, request)
}

type corpusTarget struct {
	newDialect  func() dialect.DialectPort
	emitters    func(info *dialect.Facts) map[string]func() builtinEmitter
	directories []string
}

type corpusSpec struct {
	CustomFunctions []sqlmodel.CustomFunctionConfig `json:"customFunctions"`
	TypeOverrides   []sqlmodel.TypeOverride         `json:"typeOverrides"`
	ShouldError     bool                            `json:"shouldError"`
	Skip            bool                            `json:"skip"`
}

type corpusJob struct {
	newEmitter func() builtinEmitter
	target     *corpusTarget
	spec       corpusSpec
	emitter    string
	directory  string
	location   string
}

type caseSurvey struct {
	needed     map[string]string
	reserved   []string
	unreserved []string
	failure    string
}

type localsSurvey struct {
	needed     map[string]map[string]string
	reserved   map[string]map[string]bool
	failures   []string
	unreserved []string
	generated  int
}

var corpusTargets = []corpusTarget{
	{newDialect: func() dialect.DialectPort { return postgres.New() }, emitters: withPgx,
		directories: []string{"internal/dialect/postgres/testdata", "tests/integration/postgres/testdata"}},
	{newDialect: func() dialect.DialectPort { return cockroachdb.New() }, emitters: withPgx,
		directories: []string{"internal/dialect/cockroachdb/testdata", "tests/integration/cockroachdb/testdata"}},
	{newDialect: func() dialect.DialectPort { return timescaledb.New() }, emitters: withPgx,
		directories: []string{"tests/integration/timescaledb/testdata"}},
	{newDialect: func() dialect.DialectPort { return mysql.New() }, emitters: databaseSQLEmitters,
		directories: []string{"internal/dialect/mysql/testdata", "tests/integration/mysql/testdata"}},
	{newDialect: func() dialect.DialectPort { return mariadb.New() }, emitters: databaseSQLEmitters,
		directories: []string{"internal/dialect/mariadb/testdata", "tests/integration/mariadb/testdata"}},
	{newDialect: func() dialect.DialectPort { return sqlite.New() }, emitters: databaseSQLEmitters,
		directories: []string{"internal/dialect/sqlite/testdata", "tests/integration/sqlite/testdata"}},
	{newDialect: func() dialect.DialectPort { return duckdb.New() }, emitters: databaseSQLEmitters,
		directories: []string{"internal/dialect/duckdb/testdata", "tests/integration/duckdb/testdata"}},
	{newDialect: func() dialect.DialectPort { return clickhouse.New() }, emitters: withNativeClickHouse,
		directories: []string{"internal/dialect/clickhouse/testdata", "tests/integration/clickhouse/testdata"}},
}

var surveyCorpora = sync.OnceValue(func() *localsSurvey {
	survey := &localsSurvey{needed: make(map[string]map[string]string), reserved: make(map[string]map[string]bool)}
	for index := range corpusTargets {
		survey.failures = append(survey.failures, missingCorpora(&corpusTargets[index])...)
	}
	jobs := make(chan corpusJob)
	var mutex sync.Mutex
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for job := range jobs {
				surveyed := surveyCase(&job)
				mutex.Lock()
				survey.record(job.emitter, &surveyed)
				mutex.Unlock()
			}
		})
	}
	for index := range corpusTargets {
		queueTarget(&corpusTargets[index], jobs)
	}
	close(jobs)
	workers.Wait()
	return survey
})

func (survey *localsSurvey) record(emitter string, surveyed *caseSurvey) {
	switch {
	case surveyed.failure != "":
		survey.failures = append(survey.failures, surveyed.failure)
		return
	case surveyed.reserved == nil:
		return
	}
	survey.generated++
	survey.unreserved = append(survey.unreserved, surveyed.unreserved...)
	if survey.needed[emitter] == nil {
		survey.needed[emitter] = make(map[string]string)
		survey.reserved[emitter] = make(map[string]bool)
	}
	for _, name := range surveyed.reserved {
		survey.reserved[emitter][name] = true
	}
	for name, location := range surveyed.needed {
		if _, seen := survey.needed[emitter][name]; !seen {
			survey.needed[emitter][name] = location
		}
	}
}

func (survey *localsSurvey) neededByAny() []string {
	needed := make(map[string]bool)
	for _, names := range survey.needed {
		for name := range names {
			needed[name] = true
		}
	}
	return slices.Sorted(maps.Keys(needed))
}

type checkedPackage struct {
	fileSet *token.FileSet
	info    *types.Info
	scope   *types.Scope
	files   []*ast.File
}

type qualifierImporter map[string]string

func (qualifiers qualifierImporter) Import(importPath string) (*types.Package, error) {
	name, ok := qualifiers[importPath]
	if !ok {
		name = path.Base(importPath)
	}
	imported := types.NewPackage(importPath, name)
	imported.MarkComplete()
	return imported, nil
}

type plannedName func(name string) bool

type functionNames struct {
	scope   *types.Scope
	uses    map[*ast.Ident]types.Object
	values  map[types.Object]plannedName
	planned map[types.Object]plannedName
	fixed   []types.Object
}

func (checked *checkedPackage) neededLocals(function *ast.FuncDecl, naming *emit.Naming) []string {
	names := checked.namesOf(function, naming)
	needed := make(map[string]bool)
	for identifier, object := range names.uses {
		inner := names.scope.Innermost(identifier.Pos())
		if couldBe, isPlanned := names.values[object]; isPlanned {
			for _, candidate := range names.fixed {
				if _, found := inner.LookupParent(candidate.Name(), identifier.Pos()); found == candidate && couldBe(candidate.Name()) {
					needed[candidate.Name()] = true
				}
			}
			continue
		}
		if !slices.Contains(names.fixed, object) {
			continue
		}
		for local, couldBe := range names.planned {
			if couldBe(object.Name()) && wouldCapture(local, object, inner, identifier.Pos()) {
				needed[object.Name()] = true
			}
		}
	}
	for local, couldBe := range names.planned {
		for _, candidate := range names.fixed {
			if candidate.Parent() == local.Parent() && couldBe(candidate.Name()) {
				needed[candidate.Name()] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(needed))
}

func (checked *checkedPackage) namesOf(function *ast.FuncDecl, naming *emit.Naming) *functionNames {
	names := &functionNames{
		scope:   checked.info.Scopes[function.Type],
		uses:    make(map[*ast.Ident]types.Object),
		values:  make(map[types.Object]plannedName),
		planned: make(map[types.Object]plannedName),
	}
	for identifier, object := range checked.info.Uses {
		if within(function, identifier.Pos()) {
			names.uses[identifier] = object
		}
	}
	plannedLocals := checked.plannedLocals(function, naming, names.uses)
	for _, object := range checked.localObjects(function) {
		if couldBe, isPlanned := plannedLocals[object.Name()]; isPlanned {
			names.planned[object] = couldBe
			continue
		}
		names.fixed = append(names.fixed, object)
	}
	for index := range naming.Queries {
		query := &naming.Queries[index]
		for name, couldBe := range map[string]plannedName{
			query.SQLConstant:    anyName,
			query.CountConstant:  endingIn("CountSQL"),
			query.AllowedColumns: endingIn("AllowedColumns"),
		} {
			if object := checked.scope.Lookup(name); name != "" && object != nil {
				names.values[object] = couldBe
			}
		}
	}
	maps.Copy(names.values, names.planned)
	return names
}

func (checked *checkedPackage) plannedLocals(function *ast.FuncDecl, naming *emit.Naming, uses map[*ast.Ident]types.Object) map[string]plannedName {
	used := make(map[string]bool)
	for _, object := range uses {
		if object.Parent() == checked.scope {
			used[object.Name()] = true
		}
	}
	method := queriesMethod(function)
	planned := make(map[string]plannedName)
	for index := range naming.Queries {
		query := &naming.Queries[index]
		if query.SQLConstant == "" || (!used[query.SQLConstant] && method != query.Method) {
			continue
		}
		for _, name := range query.ColumnLocals {
			planned[name] = anyName
		}
		if query.ParamsArgument != "" {
			planned[query.ParamsArgument] = exactly(query.ParamsArgument)
		}
		if query.Params == "" {
			for _, name := range query.ParameterFields {
				planned[name] = anyName
			}
		}
	}
	return planned
}

func (checked *checkedPackage) localObjects(function *ast.FuncDecl) []types.Object {
	var objects []types.Object
	for identifier, object := range checked.info.Defs {
		if object != nil && within(function, identifier.Pos()) && checked.isLocal(object) {
			objects = append(objects, object)
		}
	}
	for node, object := range checked.info.Implicits {
		if within(function, node.Pos()) && checked.isLocal(object) {
			objects = append(objects, object)
		}
	}
	return objects
}

func (checked *checkedPackage) isLocal(object types.Object) bool {
	parent := object.Parent()
	return parent != nil && parent != checked.scope && parent != types.Universe && object.Name() != "_"
}

func TestForKeepsTheDefaultNamesAndOnlyTheEmittersLocals(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	style := emit.Style{HelperPrefix: "acme", ErrorPrefix: "acme"}
	defaults := emitkit.DefaultReservation(&info, &style)

	reserved := reservation.For(&info, &style, []string{"chunk", "ctx"}, []string{"acmeExtra", "Queries"}, []string{"extra.go"})

	require.Equal(t, defaults.Receiver, reserved.Receiver)
	require.Equal(t, defaults.Imports, reserved.Imports)
	require.Equal(t, defaults.Methods, reserved.Methods)
	require.Subset(t, reserved.Declarations, defaults.Declarations)
	require.Contains(t, reserved.Declarations, "acmeExtra")
	require.Subset(t, reserved.Files, defaults.Files)
	require.Contains(t, reserved.Files, "extra.go")
	require.Contains(t, reserved.Locals, "chunk")
	require.Contains(t, reserved.Locals, "ctx")
	require.Contains(t, reserved.Locals, "rows")
	require.NotContains(t, reserved.Locals, "offset", "a local no builder of the emitter declares stays free for a parameter")
	require.NotContains(t, reserved.Locals, "source", "a helper's local is not one a query's names can meet")
	require.IsIncreasing(t, reserved.Locals)
}

func TestForReservesOnlyLocalsOfBodiesThatReferToPlannedNames(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	style := emit.Style{HelperPrefix: "acme", ErrorPrefix: "acme"}
	reserved := reservation.For(&info, &style, nil, nil, nil)

	testCases := []struct {
		name     string
		local    string
		reserved bool
	}{
		{name: "a query method's rows", local: "rows", reserved: true},
		{name: "a grouped read's index", local: "groupIndex", reserved: true},
		{name: "a grouped read's closing copy, which refers to no planned name", local: "key", reserved: false},
		{name: "a slice argument's loop element, which refers to no planned name", local: "v", reserved: false},
		{name: "a streaming read's yield", local: "yield", reserved: true},
		{name: "the runtime builder's receiver, beside its SQL constant", local: "builder", reserved: true},
		{name: "the runtime builder's filter, beside its SQL constant", local: "filter", reserved: true},
		{name: "the runtime builder's pagination, beside its SQL constant", local: "pagination", reserved: true},
		{name: "a Where argument, whose body names no planned value", local: "value", reserved: false},
		{name: "an OrderBy argument", local: "direction", reserved: false},
		{name: "a Limit argument", local: "n", reserved: false},
		{name: "a Count terminal's count, whose body names no planned value", local: "count", reserved: false},
		{name: "a terminal's items, beside only exported types", local: "items", reserved: false},
		{name: "a name no generated body declares any more", local: "resolvedColumn", reserved: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.reserved, slices.Contains(reserved.Locals, testCase.local))
		})
	}
}

func TestARuntimeBuilderNamedBuilderKeepsItsSQLConstantClearOfTheReceiver(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	testCases := []struct {
		emitter emit.ReserverPort
		name    string
	}{
		{name: "database/sql", emitter: databasesql.New()},
		{name: "pgx", emitter: pgx.New()},
		{name: "native ClickHouse", emitter: emitclickhouse.New()},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			style := emit.Style{HelperPrefix: "sgl", ErrorPrefix: "sgl"}
			request := &emit.Request{Package: "store", Style: style, Queries: []*sqlmodel.AnalysedQuery{
				{Name: "builder", Filename: "posts.sql", Command: sqlmodel.QueryCommandMany, ReadOnly: true, DynamicRuntime: true},
			}}

			naming, problems := emitkit.PlanPackage(request, testCase.emitter.Reserve(&info, &style), emitkit.NameStyle{})

			require.Empty(t, problems)
			require.Equal(t, "BuilderBuilder", naming.Queries[0].Builder)
			require.NotEqual(t, "builder", naming.Queries[0].SQLConstant)
		})
	}
}

func TestHelpersNamesHelpersAndSentinelsAfterTheStyle(t *testing.T) {
	t.Parallel()

	style := emit.Style{HelperPrefix: "acme", ErrorPrefix: "acme"}

	require.Equal(t, []string{"acmeValue", "errAcmeValue"}, reservation.Helpers(&style, []string{"Value"}, []string{"Value"}))
}

func databaseSQLEmitters(info *dialect.Facts) map[string]func() builtinEmitter {
	emitters := map[string]func() builtinEmitter{
		"databasesql prepared": func() builtinEmitter { return databasesql.New(databasesql.WithPrepared(true)) },
	}
	for _, driver := range info.Drivers {
		emitters["databasesql "+driver] = func() builtinEmitter { return databasesql.New(databasesql.WithDriver(driver)) }
	}
	return emitters
}

func withPgx(info *dialect.Facts) map[string]func() builtinEmitter {
	emitters := databaseSQLEmitters(info)
	emitters["pgx"] = func() builtinEmitter { return pgx.New() }
	return emitters
}

func withNativeClickHouse(info *dialect.Facts) map[string]func() builtinEmitter {
	emitters := databaseSQLEmitters(info)
	emitters["clickhouse"] = func() builtinEmitter { return emitclickhouse.New() }
	return emitters
}

func corpusRoot(relative string) string {
	return filepath.Join("..", "..", "..", "..", filepath.FromSlash(relative))
}

func missingCorpora(target *corpusTarget) []string {
	var missing []string
	for _, relative := range target.directories {
		if _, err := os.Stat(corpusRoot(relative)); err != nil {
			missing = append(missing, "the corpus "+relative+" cannot be read: "+err.Error())
		}
	}
	return missing
}

func queueTarget(target *corpusTarget, jobs chan<- corpusJob) {
	emitters := target.emitters(new(dialect.Wrap(target.newDialect()).Info()))
	for _, relative := range target.directories {
		root := corpusRoot(relative)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			directory := filepath.Join(root, entry.Name())
			spec, ok := readCorpusSpec(directory)
			if !ok || spec.ShouldError || spec.Skip {
				continue
			}
			for _, name := range slices.Sorted(maps.Keys(emitters)) {
				jobs <- corpusJob{target: target, spec: spec, emitter: name, newEmitter: emitters[name], directory: directory,
					location: relative + "/" + entry.Name()}
			}
		}
	}
}

func readCorpusSpec(directory string) (corpusSpec, bool) {
	var spec corpusSpec
	if _, err := os.Stat(filepath.Join(directory, "queries")); err != nil {
		return spec, false
	}
	content, err := os.ReadFile(filepath.Join(directory, "testspec.json"))
	if err != nil || json.Unmarshal(content, &spec) != nil {
		return spec, false
	}
	return spec, true
}

func surveyCase(job *corpusJob) caseSurvey {
	failed := func(err error) caseSurvey {
		return caseSurvey{failure: fmt.Sprintf("%s with %s: %v", job.location, job.emitter, err)}
	}
	recorder := &recordingEmitter{builtinEmitter: job.newEmitter(), request: nil}
	preset := app.DefaultPreset()
	preset.QueryNames = app.QueryNames{Enabled: true, File: "query_names.go", Function: "QueryName"}
	generator, err := app.New(job.target.newDialect(),
		app.WithEmitter(recorder),
		app.WithPreset(preset),
		app.WithCustomFunctions(job.spec.CustomFunctions...),
		app.WithTypeOverrides(job.spec.TypeOverrides...),
		app.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		return failed(err)
	}
	result, err := generator.Generate(context.Background(), app.Input{
		Migrations: os.DirFS(job.directory), Queries: nil, Package: "store", MigrationDir: "migrations", QueryDir: "queries",
	})
	if err != nil {
		return failed(err)
	}
	if result.Err() != nil || recorder.request == nil {
		return caseSurvey{}
	}
	request := recorder.request
	reserved := recorder.Reserve(&request.Dialect, &request.Style)
	checked, err := checkPackage(result.Files, &request.Naming)
	if err != nil {
		return failed(err)
	}
	needed := make(map[string]string)
	for _, file := range checked.files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			for _, name := range checked.neededLocals(function, &request.Naming) {
				if name != reserved.Receiver {
					needed[name] = fmt.Sprintf("%s %s %s", job.location, checked.fileSet.File(file.Pos()).Name(), function.Name.Name)
				}
			}
		}
	}
	return caseSurvey{needed: needed, reserved: slices.Clone(reserved.Locals),
		unreserved: unreservedFiles(job, result.Files, reserved.Files, preset.QueryNames.File)}
}

func unreservedFiles(job *corpusJob, generated []sqlmodel.GeneratedFile, reserved []string, queryNamesFile string) []string {
	var unreserved []string
	for _, file := range generated {
		if strings.HasSuffix(file.Name, ".sql.go") || file.Name == queryNamesFile || slices.Contains(reserved, file.Name) {
			continue
		}
		unreserved = append(unreserved, fmt.Sprintf("%s with %s writes %s, which its reservation does not list", job.location, job.emitter, file.Name))
	}
	return unreserved
}

func checkPackage(generated []sqlmodel.GeneratedFile, naming *emit.Naming) (*checkedPackage, error) {
	checked := &checkedPackage{
		fileSet: token.NewFileSet(),
		info: &types.Info{
			Defs:      make(map[*ast.Ident]types.Object),
			Uses:      make(map[*ast.Ident]types.Object),
			Implicits: make(map[ast.Node]types.Object),
			Scopes:    make(map[ast.Node]*types.Scope),
		},
	}
	for _, file := range generated {
		if !strings.HasSuffix(file.Name, ".go") {
			continue
		}
		parsed, err := parser.ParseFile(checked.fileSet, file.Name, file.Content, 0)
		if err != nil {
			return nil, err
		}
		checked.files = append(checked.files, parsed)
	}
	config := types.Config{Importer: qualifierImporter(naming.Imports), Error: func(error) {}}
	generatedPackage, _ := config.Check("store", checked.fileSet, checked.files, checked.info)
	checked.scope = generatedPackage.Scope()
	return checked, nil
}

func anyName(string) bool {
	return true
}

func endingIn(suffix string) plannedName {
	return func(name string) bool {
		return strings.HasSuffix(name, suffix)
	}
}

func exactly(planned string) plannedName {
	return func(name string) bool {
		return name == planned
	}
}

func within(node ast.Node, position token.Pos) bool {
	return position >= node.Pos() && position < node.End()
}

func wouldCapture(planned, used types.Object, inner *types.Scope, position token.Pos) bool {
	if planned.Pos() >= position {
		return false
	}
	for scope := inner; scope != nil; scope = scope.Parent() {
		switch scope {
		case used.Parent():
			return false
		case planned.Parent():
			return true
		}
	}
	return false
}

func queriesMethod(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return ""
	}
	pointer, ok := function.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return ""
	}
	if receiverType, isIdentifier := pointer.X.(*ast.Ident); isIdentifier && receiverType.Name == "Queries" {
		return function.Name.Name
	}
	return ""
}

func surveyed(t *testing.T) *localsSurvey {
	t.Helper()
	if testing.Short() {
		t.Skip("generates every corpus with every built-in emitter")
	}
	survey := surveyCorpora()
	require.Empty(t, survey.failures)
	require.Positive(t, survey.generated)
	return survey
}

func TestEveryBuiltInEmitterReservesTheLocalsItsBodiesNeed(t *testing.T) {
	t.Parallel()

	survey := surveyed(t)

	var unreserved []string
	for emitter, needed := range survey.needed {
		for name, location := range needed {
			if !survey.reserved[emitter][name] {
				unreserved = append(unreserved, fmt.Sprintf("%s leaves %s free, which %s declares where a planned name could meet it", emitter, name, location))
			}
		}
	}
	slices.Sort(unreserved)
	require.Empty(t, unreserved)
}

func TestNoBuiltInEmitterReservesALocalNoBodyNeeds(t *testing.T) {
	t.Parallel()

	survey := surveyed(t)
	needed := survey.neededByAny()

	var stale []string
	for emitter, reserved := range survey.reserved {
		for name := range reserved {
			if !slices.Contains(needed, name) {
				stale = append(stale, emitter+" reserves "+name+", which no generated body needs")
			}
		}
	}
	slices.Sort(stale)
	require.Empty(t, stale)
}

func TestDefaultReservationReservesExactlyTheLocalsTheBuiltInEmittersNeed(t *testing.T) {
	t.Parallel()

	survey := surveyed(t)
	defaults := emitkit.DefaultReservation(new(dialect.Wrap(postgres.New()).Info()), &emit.Style{HelperPrefix: "sgl", ErrorPrefix: "sgl"})

	require.Equal(t, survey.neededByAny(), slices.Sorted(slices.Values(defaults.Locals)))
}

func TestEveryFileABuiltInEmitterWritesIsReserved(t *testing.T) {
	t.Parallel()

	survey := surveyed(t)

	slices.Sort(survey.unreserved)
	require.Empty(t, survey.unreserved)
}

func TestDefaultReservationReservesEveryFileTheBuiltInEmittersReserve(t *testing.T) {
	t.Parallel()

	style := emit.Style{HelperPrefix: "sgl", ErrorPrefix: "sgl"}
	for index := range corpusTargets {
		info := dialect.Wrap(corpusTargets[index].newDialect()).Info()
		defaults := emitkit.DefaultReservation(&info, &style)
		emitters := corpusTargets[index].emitters(&info)
		for _, name := range slices.Sorted(maps.Keys(emitters)) {
			reserved := emitters[name]().Reserve(&info, &style)

			require.Subset(t, defaults.Files, reserved.Files, "%s with %s reserves a file the default reservation leaves free", info.Name, name)
		}
	}
}
