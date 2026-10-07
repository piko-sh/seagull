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

package pipeline

import (
	"cmp"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"slices"

	"seagull.sh/seagull/internal/analyse"
	cataloguebuilder "seagull.sh/seagull/internal/catalogue"
	"seagull.sh/seagull/internal/check"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/preset"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/structtag"
)

const (
	// logFieldPackage holds the structured log field name for the package being generated.
	logFieldPackage = "package"

	// DefaultMaxQueries is the default cap on the query blocks one generation analyses,
	// those that give no query included.
	DefaultMaxQueries = 100_000

	// DefaultMaxDiagnostics is the default cap on the diagnostics one run reports; the rest
	// are summarised in one Q066 diagnostic.
	DefaultMaxDiagnostics = 10_000

	// operationGenerate names a generation in a recovered panic.
	operationGenerate = "generation"

	// operationVet names a vet run in a recovered panic.
	operationVet = "vet"

	// operationCatalogue names a catalogue build in a recovered panic.
	operationCatalogue = "catalogue build"
)

// Request names the SQL one run reads. The app package adapts its public Input to it, so
// the pipeline declares no copy of the public request type.
type Request interface {
	// MigrationSource returns the migration files and their directory.
	//
	// Returns fs.FS which holds the migration files; nil when the catalogue comes from a
	// schema source.
	// Returns string which is the slash-separated directory within it; empty means the root.
	MigrationSource() (fs.FS, string)

	// QuerySource returns the query files and their directory.
	//
	// Returns fs.FS which holds the query files; nil reads them from the migration files.
	// Returns string which is the slash-separated directory within it; empty means the root.
	QuerySource() (fs.FS, string)

	// PackageName returns the Go package name of the generated files.
	//
	// Returns string which is the package name; only Generate needs it.
	PackageName() string

	// PackageImportPath returns the import path of the generated package, so a Go type an
	// override names in the package itself is written without importing it.
	//
	// Returns string which is the import path, or empty when it is not known; only Generate
	// needs it.
	PackageImportPath() string
}

// CatalogueBuilder builds the schema catalogue for one run. A schema.SchemaSourcePort
// supplies one through its BuildCatalogue method.
type CatalogueBuilder func(ctx context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error)

// Limits bounds the input one generation reads and the diagnostics it reports. A zero
// field selects its default.
type Limits struct {
	// MaxFileBytes caps the size of each migration and query file; the default is
	// sqlfile.DefaultMaxFileBytes, and it may not exceed sqlfile.MaxFileBytesCeiling.
	MaxFileBytes int64

	// MaxFiles caps the entries in the migration and query directories; the default is
	// sqlfile.DefaultMaxFiles.
	MaxFiles int

	// MaxQueries caps the query blocks one generation analyses, counting a block that gives
	// no query, such as one whose header is malformed; the default is DefaultMaxQueries.
	MaxQueries int

	// MaxTotalBytes caps the bytes one generation reads across every migration and query
	// file; the default is sqlfile.DefaultMaxTotalBytes.
	MaxTotalBytes int64

	// MaxDiagnostics caps the diagnostics one run reports; the rest are summarised in one
	// Q066 diagnostic at the highest severity among them. The default is
	// DefaultMaxDiagnostics.
	MaxDiagnostics int
}

// Config configures a Pipeline. Dialect and Emitter are required.
type Config struct {
	// Dialect parses and analyses the SQL.
	Dialect dialect.DialectPort

	// Emitter generates the Go source. It is required: the caller picks the emitter, the
	// database/sql one by default.
	Emitter emit.EmitterPort

	// BuildCatalogue, when set, builds the catalogue in place of replaying the migrations.
	BuildCatalogue CatalogueBuilder

	// RuntimeTables names the tables the migration runtime keeps in a database, which a
	// catalogue from BuildCatalogue leaves out, matched in every schema without regard to
	// case; nil leaves every table.
	RuntimeTables []string

	// Settings supplies the vocabulary of directives, diagnostics and generated code; nil
	// selects preset.DefaultSettings().
	Settings *preset.Settings

	// Clock times each generation for the trace log; nil selects observe.SystemClock().
	Clock observe.Clock

	// Logger receives the pipeline's log records; nil keeps the logger carried by the
	// context, or slog.Default().
	Logger *slog.Logger

	// Observer receives the pipeline's stage spans; nil keeps the observer carried by the
	// context, or none.
	Observer observe.ObserverPort

	// TypeOverrides maps SQL types to Go types, ahead of the preset's type slots and the
	// emitter's and built-in defaults.
	TypeOverrides []sqlmodel.TypeOverride

	// CustomFunctions declares functions the dialect does not know, such as those of a
	// loadable extension.
	CustomFunctions []sqlmodel.CustomFunctionConfig

	// Limits bounds the input one generation reads.
	Limits Limits

	// SkipFailingQueries makes Generate leave out each query with an error diagnostic of its
	// own, from its analysis or from the emitter's CheckerPort, and generate the rest of the
	// package, as Generation.Partial reports. An error diagnostic that is no one query's own
	// still stops generation.
	SkipFailingQueries bool
}

// Generation is the outcome of Generate.
type Generation struct {
	// Files holds the generated files: every query's but those left out, or none when an
	// error diagnostic stopped generation.
	Files []sqlmodel.GeneratedFile

	// Methods maps the name of each query the files generate to the name of the method that
	// runs it, as the naming plan gave it, which differs from the query's name where it
	// would meet another generated name; nil when no files were generated.
	Methods map[string]string

	// Queries holds every analysed query, those left out included.
	Queries []*sqlmodel.AnalysedQuery

	// Diagnostics holds every diagnostic of the run, in source order, each reported once, up
	// to Limits.MaxDiagnostics and then one Q066 diagnostic summarising the rest.
	Diagnostics []diag.SourceError

	// Partial reports a generation that left out at least one query with an error diagnostic
	// of its own and generated the rest, under Config.SkipFailingQueries.
	Partial bool
}

// Pipeline runs generations for one dialect, emitter and preset. It holds no state
// between runs and is safe for concurrent use.
type Pipeline struct {
	// dialect parses and analyses the SQL.
	dialect dialect.DialectPort

	// emitter generates the Go source.
	emitter emit.EmitterPort

	// buildCatalogue, when set, builds the catalogue in place of the migrations.
	buildCatalogue CatalogueBuilder

	// runtimeTables names the tables a catalogue from buildCatalogue leaves out.
	runtimeTables []string

	// clock times each generation; never nil.
	clock observe.Clock

	// logger, when set, replaces the logger carried by the context.
	logger *slog.Logger

	// observer, when set, replaces the observer carried by the context.
	observer observe.ObserverPort

	// typeOverrides maps SQL types to Go types.
	typeOverrides []sqlmodel.TypeOverride

	// customFunctions declares functions the dialect does not know.
	customFunctions []sqlmodel.CustomFunctionConfig

	// reservation holds the names the emitter's generated code uses, which the naming plan
	// avoids.
	reservation emit.Reservation

	// settings holds the validated vocabulary of directives, diagnostics and generated code.
	settings preset.Settings

	// limits bounds the input, with defaults applied.
	limits Limits

	// namedTypes reports whether the naming plan names enum and struct types and the type
	// mapping resolves them to the named types, which the generated models declare. A preset
	// whose enum or struct slot names a Go type keeps that type instead.
	namedTypes bool

	// skipFailingQueries leaves out each query with an error diagnostic of its own and
	// generates the rest.
	skipFailingQueries bool
}

// New creates a pipeline.
//
// Takes config (Config) which configures the pipeline.
//
// Returns *Pipeline which is ready to run generations.
// Returns error which wraps fault.ErrNoSource when the dialect is missing,
// fault.ErrInvalidConfig when the emitter is missing, when the settings, a limit, a type
// override or a custom function are invalid, or when the query-names file or function, or
// the interface's name, is a name the emitter already uses, or fault.ErrPanic when the
// dialect's Info or the emitter's naming reservation panicked.
func New(config Config) (*Pipeline, error) {
	if config.Dialect == nil {
		return nil, fmt.Errorf("a dialect is required: %w", fault.ErrNoSource)
	}

	settings := preset.DefaultSettings()
	if config.Settings != nil {
		settings = *config.Settings
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}

	limits, err := resolveLimits(config.Limits)
	if err != nil {
		return nil, err
	}
	if err := validateTypeOverrides(config.TypeOverrides); err != nil {
		return nil, err
	}
	if err := cataloguebuilder.ValidateCustomFunctions(config.CustomFunctions); err != nil {
		return nil, err
	}

	emitter := config.Emitter
	if emitter == nil {
		return nil, fmt.Errorf("an emitter is required: %w", fault.ErrInvalidConfig)
	}
	guarded := logging.ContextWithLogger(context.Background(), config.Logger)
	info, err := dialectInfo(guarded, config.Dialect)
	if err != nil {
		return nil, err
	}
	facts := info.Facts()
	reservation, err := reservationFor(guarded, emitter, &facts, &settings.Style)
	if err != nil {
		return nil, err
	}
	if err := validateReservedNames(reservation, &settings.Style, &facts); err != nil {
		return nil, err
	}

	return &Pipeline{
		dialect:            config.Dialect,
		emitter:            emitter,
		buildCatalogue:     config.BuildCatalogue,
		runtimeTables:      slices.Clone(config.RuntimeTables),
		clock:              cmp.Or(config.Clock, observe.SystemClock()),
		logger:             config.Logger,
		observer:           config.Observer,
		typeOverrides:      config.TypeOverrides,
		customFunctions:    config.CustomFunctions,
		settings:           settings,
		reservation:        reservation,
		limits:             limits,
		namedTypes:         true,
		skipFailingQueries: config.SkipFailingQueries,
	}, nil
}

// Header returns the first line of every file the pipeline generates.
//
// Returns string which is the preset's header.
func (p *Pipeline) Header() string {
	return p.settings.Style.Header
}

// Generate builds the catalogue, analyses queries and emits Go source.
//
// SkipFailingQueries omits queries with their own error diagnostics. Errors affecting
// migrations, the catalogue or the package still stop generation.
//
// Takes request (Request) which names the migrations, the queries and the package.
//
// Returns Generation which holds the generated files, which are empty when generation
// stopped, the analysed queries, and every diagnostic of the run, in source order, each
// reported once.
// Returns error when there is a fatal problem: invalid input, unreadable files, a limit
// exceeded, cancellation, an emitter failure, or a panic recovered from a host or dialect
// call, which wraps fault.ErrPanic. Error-severity diagnostics are not errors.
func (p *Pipeline) Generate(ctx context.Context, request Request) (Generation, error) {
	return p.generate(ctx, request, nil)
}

// Vet builds the catalogue and analyses every query without emitting code. Where the
// analysis leaves queries to generate, it also asks an emitter that implements
// emit.CheckerPort what it would refuse, as Generate does before emitting, so a query the
// emitter cannot serve is reported by vet as well.
//
// Takes request (Request) which names the migrations and the queries; the package name
// names the package the emitter would check, and a name that is no Go identifier, or the
// blank identifier, skips that check.
//
// Returns []*sqlmodel.AnalysedQuery which holds the analysed queries.
// Returns []diag.SourceError which holds every diagnostic of the run, in source order,
// each reported once.
// Returns error when there is a fatal problem, as Generate does.
func (p *Pipeline) Vet(
	ctx context.Context,
	request Request,
) (queries []*sqlmodel.AnalysedQuery, diagnostics []diag.SourceError, err error) {
	ctx = p.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageVet)
	defer func() { end(err) }()
	err = panicguard.Run(ctx, operationVet, func() error {
		var vetErr error
		queries, diagnostics, vetErr = p.vet(ctx, request)
		return vetErr
	})
	if err != nil {
		return nil, nil, err
	}
	return queries, diagnostics, nil
}

// Catalogue builds the schema catalogue alone, from the schema source when the pipeline
// has one and by replaying the migrations otherwise, with the custom functions and column
// type overrides in place, as the analysis sees it.
//
// Takes request (Request) which names the migrations; the queries and package are
// ignored.
//
// Returns *sqlmodel.Catalogue which holds the schema.
// Returns []diag.SourceError which holds the diagnostics found while building it, each
// reported once.
// Returns error when there is a fatal problem.
func (p *Pipeline) Catalogue(ctx context.Context, request Request) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	ctx = p.contextFor(ctx)
	var (
		catalogue   *sqlmodel.Catalogue
		diagnostics []diag.SourceError
	)
	err := panicguard.Run(ctx, operationCatalogue, func() error {
		var buildErr error
		catalogue, diagnostics, buildErr = p.buildCatalogueFor(ctx, request, sqlfile.NewByteBudget(p.limits.MaxTotalBytes))
		return buildErr
	})
	if err != nil {
		return nil, nil, err
	}
	return catalogue, collectDiagnostics(p.limits.MaxDiagnostics, diagnostics), nil
}

// vet analyses every query and asks the emitter's CheckerPort what it refuses, as Vet
// describes.
//
// Takes request (Request) which names the migrations and the queries.
//
// Returns []*sqlmodel.AnalysedQuery which holds the analysed queries.
// Returns []diag.SourceError which holds every diagnostic of the run.
// Returns error when there is a fatal problem.
func (p *Pipeline) vet(ctx context.Context, request Request) ([]*sqlmodel.AnalysedQuery, []diag.SourceError, error) {
	analysed, err := p.analyse(ctx, request)
	if err != nil {
		return nil, nil, err
	}
	packageName := request.PackageName()
	if !preset.IsDeclarableName(packageName) || (fault.HasErrors(analysed.diagnostics) && (!p.skipFailingQueries || analysed.packageErrors)) {
		return analysed.queries, analysed.diagnostics, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	generated := generatedPackage{name: packageName, path: request.PackageImportPath()}
	_, checked, _ := p.checkedRequest(ctx, generated, analysed.catalogue, analysed.acceptedQueries())
	analysed.collector.add(checked...)
	return analysed.queries, analysed.collector.collected(), nil
}

// contextFor attaches the pipeline's logger and observer, when set, to a context.
//
// Takes ctx (context.Context) which is the caller's context.
//
// Returns context.Context which carries the logger and observer.
func (p *Pipeline) contextFor(ctx context.Context) context.Context {
	if p.logger != nil {
		ctx = logging.ContextWithLogger(ctx, p.logger)
	}
	if p.observer != nil {
		ctx = observe.ContextWithObserver(ctx, p.observer)
	}
	return ctx
}

// analyse builds the catalogue and analyses every query. The migration and query files it
// reads share one byte budget, Limits.MaxTotalBytes, and the diagnostics it reports are
// collected under Limits.MaxDiagnostics.
//
// Takes request (Request) which names the migrations and the queries.
//
// Returns analysis which holds the catalogue, the analysed queries, every diagnostic, and
// which errors are some query's own.
// Returns error when there is a fatal problem.
func (p *Pipeline) analyse(ctx context.Context, request Request) (analysis, error) {
	budget := sqlfile.NewByteBudget(p.limits.MaxTotalBytes)
	queryFiles, queryDirectory, err := p.querySource(request, budget)
	if err != nil {
		return analysis{}, err
	}

	catalogue, catalogueDiagnostics, err := p.buildCatalogueFor(ctx, request, budget)
	if err != nil {
		return analysis{}, err
	}

	collector := newDiagnosticCollector(p.limits.MaxDiagnostics)
	collector.add(catalogueDiagnostics...)
	analysed, err := p.analyseQueries(ctx, catalogue, queryFiles, queryDirectory, collector)
	if err != nil {
		return analysis{}, fmt.Errorf("analysing queries: %w", err)
	}

	depthDiagnostics := depthLimitDiagnostics(catalogue, analysed.queries, p.typeOverrides)
	collector.add(depthDiagnostics...)
	analysed.catalogue = catalogue
	analysed.catalogueDiagnostics = catalogueDiagnostics
	analysed.collector = collector
	analysed.packageErrors = analysed.packageErrors || fault.HasErrors(catalogueDiagnostics) || fault.HasErrors(depthDiagnostics)
	analysed.diagnostics = collector.collected()
	return analysed, nil
}

// querySource resolves the file system and directory the queries are read from.
//
// Takes request (Request) which names the queries.
// Takes budget (*sqlfile.ByteBudget) which caps the bytes the run reads in total.
//
// Returns sqlfile.FileReader which reads the query files.
// Returns string which is the cleaned query directory.
// Returns error which wraps fault.ErrNoSource when there is no query file system, or
// fault.ErrInvalidConfig when the directory is not a valid path.
func (p *Pipeline) querySource(request Request, budget *sqlfile.ByteBudget) (sqlfile.FileReader, string, error) {
	queries, queryDirectory := request.QuerySource()
	if queries == nil {
		queries, _ = request.MigrationSource()
	}
	if queries == nil {
		return nil, "", fmt.Errorf("no query files to read: %w", fault.ErrNoSource)
	}
	directory, err := cleanDirectory("query", queryDirectory)
	if err != nil {
		return nil, "", err
	}
	return sqlfile.NewFSReader(queries, p.limits.MaxFileBytes, p.limits.MaxFiles).WithBudget(budget), directory, nil
}

// buildCatalogueFor builds the schema catalogue from the schema source, or by replaying
// the migrations, and applies the custom functions and the column type overrides.
//
// The migrations are replayed onto a catalogue that already holds the custom functions
// and sets the overrides before each view, so views and function bodies are analysed with
// them. A schema source's catalogue is adopted as adoptSchemaSource describes.
//
// Takes request (Request) which names the migrations.
// Takes budget (*sqlfile.ByteBudget) which caps the bytes the run reads in total.
//
// Returns *sqlmodel.Catalogue which holds the schema.
// Returns []diag.SourceError which holds any diagnostics.
// Returns error when the catalogue cannot be built.
func (p *Pipeline) buildCatalogueFor(
	ctx context.Context,
	request Request,
	budget *sqlfile.ByteBudget,
) (catalogue *sqlmodel.Catalogue, diagnostics []diag.SourceError, err error) {
	ctx, end := observe.From(ctx).Start(ctx, observe.StageCatalogue)
	defer func() { end(err) }()

	info := p.dialect.Info()
	if p.buildCatalogue != nil {
		return p.adoptSchemaSource(ctx, &info)
	}
	catalogue, diagnostics, err = p.replayMigrations(ctx, request, budget)
	if err != nil {
		return nil, nil, err
	}
	cataloguebuilder.Adopt(catalogue, &info)
	diagnostics = append(diagnostics, cataloguebuilder.ApplyColumnTypeOverrides(catalogue, p.typeOverrides)...)
	diagnostics = append(diagnostics, p.unmatchedTypeOverrides(catalogue)...)
	return catalogue, diagnostics, nil
}

// adoptSchemaSource prepares a source catalogue for query analysis.
//
// It works on a copy, drops the nil entries a source may leave in it, removes runtime
// tables, and applies configured functions and type overrides before resolving view
// definitions.
//
// Takes info (*dialect.Info) which describes the dialect.
//
// Returns *sqlmodel.Catalogue which holds the schema.
// Returns []diag.SourceError which holds the source's diagnostics and the overrides'.
// Returns error when the source fails, panics or returns no catalogue, or the context is
// cancelled.
func (p *Pipeline) adoptSchemaSource(ctx context.Context, info *dialect.Info) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	catalogue, diagnostics, err := p.callSchemaSource(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("building catalogue: %w", err)
	}
	if catalogue == nil {
		return nil, nil, fmt.Errorf("the schema source returned no catalogue: %w", fault.ErrNoSource)
	}
	catalogue = cataloguebuilder.Clone(catalogue)
	dropNilEntries(catalogue)
	cataloguebuilder.Adopt(catalogue, info)
	cataloguebuilder.LeaveOutTables(catalogue, p.runtimeTables)
	cataloguebuilder.MergeCustomFunctions(catalogue, p.dialect, p.customFunctions)
	diagnostics = append(slices.Clip(diagnostics), cataloguebuilder.ApplyColumnTypeOverrides(catalogue, p.typeOverrides)...)
	diagnostics = append(diagnostics, p.unmatchedTypeOverrides(catalogue)...)
	viewDiagnostics, err := cataloguebuilder.TypeViewsFromDefinitions(ctx, p.dialect, catalogue)
	if err != nil {
		return nil, nil, fmt.Errorf("typing views: %w", err)
	}
	return catalogue, append(diagnostics, viewDiagnostics...), nil
}

// replayMigrations builds the catalogue by replaying the request's migrations onto one
// seeded with the custom functions and column overrides.
//
// Takes request (Request) which names the migrations.
// Takes budget (*sqlfile.ByteBudget) which caps the bytes the run reads in total.
//
// Returns *sqlmodel.Catalogue which holds the schema.
// Returns []diag.SourceError which holds the replay's diagnostics.
// Returns error which wraps fault.ErrNoSource when there are no migrations, or the
// failure to read them.
func (p *Pipeline) replayMigrations(
	ctx context.Context,
	request Request,
	budget *sqlfile.ByteBudget,
) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	migrations, migrationDirectory := request.MigrationSource()
	if migrations == nil {
		return nil, nil, fmt.Errorf("no migrations or schema source to build the catalogue from: %w", fault.ErrNoSource)
	}
	directory, err := cleanDirectory("migration", migrationDirectory)
	if err != nil {
		return nil, nil, err
	}
	reader := sqlfile.NewFSReader(migrations, p.limits.MaxFileBytes, p.limits.MaxFiles).WithBudget(budget)
	provider := cataloguebuilder.NewMigrationCatalogueProvider(p.dialect, reader, directory, p.settings.Prefixes,
		cataloguebuilder.Seed{CustomFunctions: p.customFunctions, ColumnOverrides: p.typeOverrides})
	catalogue, diagnostics, err := provider.BuildCatalogue(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("building catalogue: %w", err)
	}
	return catalogue, diagnostics, nil
}

// analyseQueries reads and analyses query files one at a time, retaining the analysed
// queries and their SQL in the result.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema.
// Takes reader (sqlfile.FileReader) which reads the query files.
// Takes directory (string) which is the query directory.
// Takes collector (*diagnosticCollector) which receives the queries' diagnostics.
//
// Returns analysis which holds the analysed queries, the queries and blocks with error
// diagnostics of their own, and whether two queries share a name.
// Returns error when the files cannot be read, the query limit is exceeded, the emitter
// panics, or the context is cancelled.
func (p *Pipeline) analyseQueries(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	reader sqlfile.FileReader,
	directory string,
	collector *diagnosticCollector,
) (analysed analysis, err error) {
	ctx, end := observe.From(ctx).Start(ctx, observe.StageAnalyse)
	defer func() { end(err) }()

	filenames, err := sqlfile.ListQueryFiles(ctx, reader, directory)
	if err != nil {
		return analysis{}, err
	}

	analyser := analyse.NewQueryAnalyser(p.dialect, catalogue, directive.PrefixPolicy{
		Prefixes:         p.settings.Prefixes,
		RequireCanonical: p.settings.RequireCanonical,
	})
	elements := p.elementNullability(catalogue)
	parameterElements, err := p.parameterElementNullability(ctx, elements)
	if err != nil {
		return analysis{}, err
	}
	analyser.SetElementNullability(elements)
	analyser.SetParameterElementNullability(parameterElements)
	querySyntax := p.querySyntax()

	analysed.collector = collector
	for _, filename := range filenames {
		if fileErr := p.analyseQueryFile(ctx, analyser, reader, directory, filename, querySyntax, &analysed); fileErr != nil {
			return analysis{}, fileErr
		}
	}

	duplicates := check.ValidateDuplicateNames(analysed.queries)
	analysed.packageErrors = fault.HasErrors(duplicates)
	collector.add(duplicates...)
	return analysed, nil
}

// analyseQueryFile reads one query file and analyses each of its queries, recording each
// query, and each block without one, whose own diagnostics include an error.
//
// Every block counts against Limits.MaxQueries, a block that gives no query included, so
// a file of malformed headers cannot get past the limit.
//
// Takes analyser (*analyse.QueryAnalyser) which analyses each query.
// Takes reader (sqlfile.FileReader) which reads the file.
// Takes directory (string) which is the query directory.
// Takes filename (string) which is the file's name within it.
// Takes querySyntax (sqlfile.QuerySyntax) which says how the file is written.
// Takes analysed (*analysis) which receives the file's queries, and whose collector
// receives their diagnostics.
//
// Returns error when the file cannot be read, the query limit is exceeded, or the context
// is cancelled.
func (p *Pipeline) analyseQueryFile(
	ctx context.Context,
	analyser *analyse.QueryAnalyser,
	reader sqlfile.FileReader,
	directory, filename string,
	querySyntax sqlfile.QuerySyntax,
	analysed *analysis,
) error {
	filePath := path.Join(directory, filename)
	content, err := reader.ReadFile(ctx, filePath)
	if err != nil {
		return fmt.Errorf("reading query file %s: %w", filePath, err)
	}
	blocks, truncatedLine := sqlfile.SplitQueryFile(content, querySyntax)
	if truncatedLine > 0 {
		analysed.collector.add(syntax.TruncatedTextDiagnostic(diagbuild.NewErrorBuilder(filename), truncatedLine, "the query file"))
	}
	analysed.blocks += len(blocks)
	if analysed.blocks > p.limits.MaxQueries {
		return fmt.Errorf("more than %d query blocks: %w", p.limits.MaxQueries, fault.ErrLimit)
	}
	for index := range blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		query, blockDiagnostics := analyser.AnalyseQuery(ctx, blocks[index], filename)
		analysed.collector.add(blockDiagnostics...)
		failed := fault.HasErrors(blockDiagnostics)
		if query == nil {
			if failed {
				analysed.failedBlocks++
			}
			continue
		}
		analysed.queries = append(analysed.queries, query)
		if failed {
			analysed.refuse(query)
		}
	}
	return nil
}

// querySyntax describes how the query files are written: the dialect's lexical rules and
// directive comment prefix, and the run's directive prefixes and roles.
//
// Returns sqlfile.QuerySyntax which SplitQueryFile reads a query file with.
func (p *Pipeline) querySyntax() sqlfile.QuerySyntax {
	info := p.dialect.Info()
	return sqlfile.QuerySyntax{
		Prefixes:   p.settings.Prefixes,
		Directives: directive.Roles(),
		Lexicon:    syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical},
	}
}

// emitAccepted emits the accepted queries.
//
// With Config.SkipFailingQueries, queries rejected by the emitter's CheckerPort are
// removed and emission is retried. Retrying stops when every remaining query is accepted
// or the rejection identifies no remaining query to remove, including a rejection of the
// whole request. Each retry removes at least one query.
//
// Takes generated (generatedPackage) which names the generated package.
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries to emit.
// Takes collector (*diagnosticCollector) which receives every emission diagnostic.
// Takes firstCheck (func()) which is called once the first checker diagnostics are
// collected, before emission retries.
//
// Returns emission which holds the files, how many queries the CheckerPort left out, and
// whether an error stopped emission.
// Returns error when the context is cancelled, the emitter fails or panics, or it
// generates two files of one name.
func (p *Pipeline) emitAccepted(
	ctx context.Context,
	generated generatedPackage,
	catalogue *sqlmodel.Catalogue,
	queries []*sqlmodel.AnalysedQuery,
	collector *diagnosticCollector,
	firstCheck func(),
) (emission, error) {
	var outcome emission
	for {
		emitted, err := p.emit(ctx, generated, catalogue, queries, collector, firstCheck)
		firstCheck = nil
		if err != nil {
			return emission{}, err
		}
		if !fault.HasErrors(emitted.diagnostics) {
			outcome.files, outcome.methods = emitted.files, emitted.methods
			return outcome, nil
		}
		remaining := withoutQueries(queries, emitted.refused)
		if !p.skipFailingQueries || len(remaining) == len(queries) {
			outcome.stopped = true
			return outcome, nil
		}
		outcome.leftOut += len(queries) - len(remaining)
		queries = remaining
	}
}

// generate runs emission, optionally retaining the detailed snapshots, turning a panic
// that escapes a host or dialect call into an error.
//
// Takes request (Request) which holds the input sources and generation settings.
// Takes details (*DetailedGeneration) which receives optional snapshots, or is nil for
// ordinary generation.
//
// Returns generation (Generation) which holds the generated files and diagnostics.
// Returns err (error) when the pipeline cannot complete.
func (p *Pipeline) generate(ctx context.Context, request Request, details *DetailedGeneration) (generation Generation, err error) {
	packageName := request.PackageName()
	ctx = p.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageGenerate, slog.String(logFieldPackage, packageName))
	defer func() { end(err) }()
	return panicguard.RunValue(ctx, operationGenerate, func() (Generation, error) {
		return p.runGeneration(ctx, request, details)
	})
}

// runGeneration analyses the queries and emits the accepted ones, as generate describes.
//
// Takes request (Request) which holds the input sources and generation settings.
// Takes details (*DetailedGeneration) which receives optional snapshots, or is nil.
//
// Returns Generation which holds the generated files and diagnostics.
// Returns error when the pipeline cannot complete.
func (p *Pipeline) runGeneration(ctx context.Context, request Request, details *DetailedGeneration) (Generation, error) {
	packageName := request.PackageName()
	logger := logging.LoggerFrom(ctx)
	started := p.clock.Now()

	if !preset.IsDeclarableName(packageName) {
		return Generation{}, fmt.Errorf("package name %q is not a Go identifier other than the blank identifier: %w", packageName, fault.ErrInvalidConfig)
	}

	analysed, err := p.analyse(ctx, request)
	if err != nil {
		return Generation{}, err
	}
	if details != nil {
		details.Catalogue = cataloguebuilder.Clone(analysed.catalogue)
		details.CatalogueDiagnostics = collectDiagnostics(p.limits.MaxDiagnostics, analysed.catalogueDiagnostics)
	}
	generation := Generation{Files: nil, Queries: analysed.queries, Diagnostics: analysed.diagnostics, Methods: nil, Partial: false}

	if fault.HasErrors(analysed.diagnostics) && (!p.skipFailingQueries || analysed.packageErrors) {
		if details != nil {
			details.capture(analysed)
		}
		logStopped(ctx, packageName, len(generation.Diagnostics))
		return generation, nil
	}

	var firstCheck func()
	if details != nil {
		firstCheck = func() { details.capture(analysed) }
	}
	generated := generatedPackage{name: packageName, path: request.PackageImportPath()}
	emitted, err := p.emitAccepted(ctx, generated, analysed.catalogue, analysed.acceptedQueries(), analysed.collector, firstCheck)
	if err != nil {
		return Generation{}, err
	}
	generation.Diagnostics = analysed.collector.collected()
	if emitted.stopped {
		logStopped(ctx, packageName, len(generation.Diagnostics))
		return generation, nil
	}
	generation.Files = emitted.files
	generation.Methods = emitted.methods
	generation.Partial = len(analysed.refused) > 0 || analysed.failedBlocks > 0 || emitted.leftOut > 0

	logger.Log(ctx, logging.LevelTrace, "generation complete",
		slog.String(logFieldPackage, packageName),
		slog.Int("files", len(generation.Files)),
		slog.Int("queries", len(generation.Queries)),
		slog.Bool("partial", generation.Partial),
		slog.Duration("elapsed", p.clock.Now().Sub(started)),
	)
	return generation, nil
}

// checkedRequest builds the request an emitter would emit: it builds the type mapping
// table, plans the package's names, and asks an emitter that implements emit.CheckerPort
// what it refuses.
//
// Takes generated (generatedPackage) which names the generated package.
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the analysed queries.
//
// Returns *emit.Request which is the request.
// Returns []diag.SourceError which holds a diagnostic for every name that cannot be
// planned and for every problem the emitter's CheckerPort finds, or one Q070 error when
// the emitter's DefaultTypes or Check panicked; the CheckerPort is not asked when a name
// cannot be planned.
// Returns []*sqlmodel.AnalysedQuery which holds the queries the plan or the CheckerPort
// refused with an error, when every error is one query's own, and is nil otherwise.
func (p *Pipeline) checkedRequest(
	ctx context.Context,
	generated generatedPackage,
	catalogue *sqlmodel.Catalogue,
	queries []*sqlmodel.AnalysedQuery,
) (*emit.Request, []diag.SourceError, []*sqlmodel.AnalysedQuery) {
	_, end := observe.From(ctx).Start(ctx, observe.StagePlan)
	defer end(nil)
	info := p.dialect.Info()
	facts := info.Facts()
	emitterSlots, slotsErr := p.emitterSlots(ctx, &facts)
	mappings := gotypes.NewTypeMapper(p.dialect.BuiltinTypes()).WithSchema(catalogue).BuildMappingTable(p.typeOverrides, emitterSlots, p.settings.Types)

	request := &emit.Request{
		Catalogue:         modelCatalogue(catalogue, p.elementNullability(catalogue)),
		Types:             mappings,
		Package:           generated.name,
		Dialect:           facts,
		Queries:           queries,
		DirectivePrefixes: p.settings.Prefixes.Accepted(),
		Style:             p.settings.Style,
		Naming:            emit.Naming{},
	}
	finishGoTypes(request, mappings, generated.path)
	if slotsErr != nil {
		return request, []diag.SourceError{hostFailure(slotsErr)}, nil
	}
	planProblems := p.planNaming(request, mappings)
	diagnostics := problemDiagnostics(planProblems)
	if fault.HasErrors(diagnostics) {
		return request, diagnostics, refusedQueries(planProblems)
	}
	checker, ok := p.emitter.(emit.CheckerPort)
	if !ok {
		return request, diagnostics, nil
	}
	problems, err := callCheck(ctx, checker, request)
	if err != nil {
		return request, append(diagnostics, hostFailure(err)), nil
	}
	return request, append(diagnostics, problemDiagnostics(problems)...), refusedQueries(problems)
}

// emit builds the type mapping table, plans the package's names, asks an emitter that
// implements emit.CheckerPort what it refuses, and asks the emitter for the Go source.
//
// Takes generated (generatedPackage) which names the generated package.
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the analysed queries.
// Takes collector (*diagnosticCollector) which receives the attempt's diagnostics.
// Takes firstCheck (func()) which, when set, is called once the attempt's diagnostics are
// collected and before the emitter is asked for the Go source.
//
// Returns attempt which holds the generated files, or none when a name cannot be planned
// or the emitter refuses the request; a diagnostic for every name that cannot be planned
// and for every problem the emitter's CheckerPort finds, at its query's file and line,
// the emitter not being asked to emit when any is an error; the queries the plan or the
// CheckerPort refused with an error, when every error is one query's own; and the method
// the plan names for each query.
// Returns error when the context is cancelled, the emitter fails or panics, which the
// error wraps with the package's name, or it generates two files of one name.
func (p *Pipeline) emit(
	ctx context.Context,
	generated generatedPackage,
	catalogue *sqlmodel.Catalogue,
	queries []*sqlmodel.AnalysedQuery,
	collector *diagnosticCollector,
	firstCheck func(),
) (attempt, error) {
	if err := ctx.Err(); err != nil {
		return attempt{}, err
	}

	request, diagnostics, refused := p.checkedRequest(ctx, generated, catalogue, queries)
	collector.add(diagnostics...)
	if firstCheck != nil {
		firstCheck()
	}
	if fault.HasErrors(diagnostics) {
		return attempt{files: nil, diagnostics: diagnostics, refused: refused, methods: nil}, nil
	}
	emitContext, end := observe.From(ctx).Start(ctx, observe.StageEmit)
	files, err := p.callEmit(emitContext, request)
	end(err)
	if err != nil {
		return attempt{}, fmt.Errorf("emitting package %s: %w", generated.name, err)
	}
	if err := checkGeneratedFileNames(files); err != nil {
		return attempt{}, err
	}
	return attempt{files: files, diagnostics: diagnostics, refused: nil, methods: plannedMethods(request)}, nil
}

// resolveLimits applies the defaults to the zero fields of a Limits and rejects negative
// ones.
//
// Takes configured (Limits) which are the configured limits.
//
// Returns Limits which has every field set.
// Returns error which wraps fault.ErrInvalidConfig when a limit is negative, or the file
// size limit is above sqlfile.MaxFileBytesCeiling.
func resolveLimits(configured Limits) (Limits, error) {
	if configured.MaxFileBytes < 0 || configured.MaxFiles < 0 || configured.MaxQueries < 0 || configured.MaxTotalBytes < 0 ||
		configured.MaxDiagnostics < 0 {
		return Limits{}, fmt.Errorf("limits must not be negative: %w", fault.ErrInvalidConfig)
	}
	if configured.MaxFileBytes > sqlfile.MaxFileBytesCeiling {
		return Limits{}, fmt.Errorf("the file size limit %d is above the ceiling of %d bytes: %w",
			configured.MaxFileBytes, sqlfile.MaxFileBytesCeiling, fault.ErrInvalidConfig)
	}
	if configured.MaxFileBytes == 0 {
		configured.MaxFileBytes = sqlfile.DefaultMaxFileBytes
	}
	if configured.MaxFiles == 0 {
		configured.MaxFiles = sqlfile.DefaultMaxFiles
	}
	if configured.MaxQueries == 0 {
		configured.MaxQueries = DefaultMaxQueries
	}
	if configured.MaxTotalBytes == 0 {
		configured.MaxTotalBytes = sqlfile.DefaultMaxTotalBytes
	}
	if configured.MaxDiagnostics == 0 {
		configured.MaxDiagnostics = DefaultMaxDiagnostics
	}
	return configured, nil
}

// validateTypeOverrides validates override targets, Go types, element nullability and
// struct tags. Each override must name exactly one SQL type or column and specify at
// least one supported customisation; struct tags require a column target.
//
// Takes overrides ([]sqlmodel.TypeOverride) which are the configured overrides.
//
// Returns error which wraps fault.ErrInvalidConfig for the first unusable override.
func validateTypeOverrides(overrides []sqlmodel.TypeOverride) error {
	for index, override := range overrides {
		if (override.SQLTypeName == "") == (override.Column == "") {
			return fmt.Errorf("type override %d must name exactly one of a SQL type and a column: %w",
				index, fault.ErrInvalidConfig)
		}
		if override.GoName == "" && override.NonNullElements == nil && len(override.StructTags) == 0 {
			return fmt.Errorf("type override %d has no Go type name, no non_null_elements and no struct tags: %w", index, fault.ErrInvalidConfig)
		}
		if err := structtag.CheckAll(override.StructTags); err != nil {
			return fmt.Errorf("type override %d: %w: %w", index, err, fault.ErrInvalidConfig)
		}
		if override.Column == "" {
			if len(override.StructTags) > 0 {
				return fmt.Errorf("type override %d gives struct tags to the SQL type %s; only a column override takes them: %w",
					index, override.SQLTypeName, fault.ErrInvalidConfig)
			}
			continue
		}
		if _, err := cataloguebuilder.ParseColumnReference(override.Column); err != nil {
			return fmt.Errorf("type override %d: %w", index, err)
		}
	}
	return nil
}

// cleanDirectory normalises a slash-separated directory within a file system, with empty
// meaning the root.
//
// Takes role (string) which names the directory in error messages.
// Takes directory (string) which is the configured directory.
//
// Returns string which is the cleaned directory.
// Returns error which wraps fault.ErrInvalidConfig when the directory is not a valid
// io/fs path.
func cleanDirectory(role, directory string) (string, error) {
	if directory == "" {
		return ".", nil
	}
	cleaned := path.Clean(directory)
	if !fs.ValidPath(cleaned) {
		return "", fmt.Errorf("%s directory %q is not a valid relative, slash-separated path: %w",
			role, directory, fault.ErrInvalidConfig)
	}
	return cleaned, nil
}

// logStopped records, for diagnosing Seagull itself, that error diagnostics stopped a
// generation. The caller reports the diagnostics, so nothing is logged above
// logging.LevelInternal.
//
// Takes packageName (string) which names the generated package.
// Takes diagnostics (int) which is how many diagnostics the run reported.
func logStopped(ctx context.Context, packageName string, diagnostics int) {
	logging.LoggerFrom(ctx).Log(ctx, logging.LevelInternal, "generation stopped by error diagnostics",
		slog.String(logFieldPackage, packageName),
		slog.Int("diagnostics", diagnostics),
	)
}
