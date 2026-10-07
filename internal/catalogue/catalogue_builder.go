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

package catalogue

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/structtag"
)

// catalogueBuilder constructs a Catalogue by replaying DDL mutations from migration
// files. Each migration file is parsed by the dialect and the resulting mutations are
// applied sequentially to build up the schema state.
type catalogueBuilder struct {
	// engine holds the dialect used for parsing and DDL interpretation.
	engine dialect.DialectPort

	// catalogue holds the schema state being built up by replaying mutations.
	catalogue *sqlmodel.Catalogue

	// calledFunctions maps each catalogue function to the names called by its body for
	// data-access analysis.
	calledFunctions map[*sqlmodel.Function][]string

	// calledTableFunctions maps each catalogue function whose body was analysed to the names
	// of the table-valued functions the body reads as FROM items.
	calledTableFunctions map[*sqlmodel.Function][]string

	// prefixes holds the directive prefixes recognised in migration comments.
	prefixes namespace.Set

	// columnOverrides holds the configured type overrides that name a column, read once;
	// each is set on its column before each view is analysed, so the view's columns carry
	// them.
	columnOverrides []configuredColumnOverride

	// pendingColumnDirectives holds the column directives of the migration being applied,
	// read once; they are set on the columns that exist before each view is analysed, and
	// reported once the whole migration is applied.
	pendingColumnDirectives []columnDirective

	// statementDiagnostics collects the diagnostics found while applying the current
	// statement's mutations, such as an unknown function in a view.
	statementDiagnostics []diag.SourceError

	// partitionParents maps each partition the migrations create or attach to the table it
	// partitions.
	partitionParents map[*sqlmodel.Table]*sqlmodel.Table

	// inheritances records which tables inherit the columns of which, so a change to a
	// parent's columns reaches its children.
	inheritances []inheritance

	// followingViews holds, in the order they were created, the views of an engine whose
	// Info lists ViewsFollowTheirTables, with the definitions their columns are derived from
	// again after each later change to a table or view.
	followingViews []followingView

	// localColumns holds, for each table that inherits columns, the keys of the columns it
	// declares itself, which a parent's DROP COLUMN leaves in place.
	localColumns map[*sqlmodel.Table]map[string]struct{}

	// statement locates the statement being applied, for its diagnostics.
	statement statementSite

	// searchPaths tracks the search path the migrations replay with.
	searchPaths searchPaths

	// lexicon is how the dialect writes SQL, read once from its Info.
	lexicon syntax.Lexicon
}

var (
	// mutationHandlers maps DDL mutation kinds to their catalogue builder handler functions.
	// Mutation kinds intentionally absent from this table fall through to the explicit
	// switch in dispatchMutation (MutationCreateTable, MutationCreateFunction,
	// MutationCreateView), whose handlers analyse queries, or are silently ignored as no-ops
	// via catalogueMutationNoOp; see noOpMutations for the deliberate list.
	mutationHandlers map[ast.MutationKind]func(*catalogueBuilder, *ast.CatalogueMutation) error

	// noOpMutations enumerates the mutation kinds the catalogue builder deliberately
	// ignores.
	//
	// Their effects are not represented in the catalogue, including any schema changes made
	// by the listed column, query, exchange, attach, detach or restore operations.
	noOpMutations = [...]ast.MutationKind{
		ast.MutationCreateTrigger,
		ast.MutationDropTrigger,
		ast.MutationAsyncDataUpdate,
		ast.MutationAsyncDataDelete,
		ast.MutationCreateDictionary,
		ast.MutationDropDictionary,
		ast.MutationExchangeTables,
		ast.MutationAlterTableAddProjection,
		ast.MutationAlterTableDropProjection,
		ast.MutationAlterTableMaterializeProjection,
		ast.MutationAlterTableAddSkippingIndex,
		ast.MutationAlterTableDropSkippingIndex,
		ast.MutationAlterTableMaterializeIndex,
		ast.MutationAlterTableAddStatistics,
		ast.MutationAlterTableDropStatistics,
		ast.MutationAlterTableMaterializeStatistics,
		ast.MutationAlterTableModifyStatistics,
		ast.MutationAlterTableMaterializeColumn,
		ast.MutationAlterTableModifyColumn,
		ast.MutationAlterTableModifyQuery,
		ast.MutationAlterTableModifyRefresh,
		ast.MutationAlterTablePartition,
		ast.MutationCreateUser,
		ast.MutationAlterUser,
		ast.MutationDropUser,
		ast.MutationCreateRole,
		ast.MutationAlterRole,
		ast.MutationDropRole,
		ast.MutationCreatePolicy,
		ast.MutationAlterPolicy,
		ast.MutationDropPolicy,
		ast.MutationCreateQuota,
		ast.MutationAlterQuota,
		ast.MutationDropQuota,
		ast.MutationCreateSettingsProfile,
		ast.MutationAlterSettingsProfile,
		ast.MutationDropSettingsProfile,
		ast.MutationGrantManagement,
		ast.MutationAttachTable,
		ast.MutationDetachTable,
		ast.MutationBackup,
		ast.MutationRestore,
		ast.MutationKillQuery,
		ast.MutationKillMutation,
	}
)

// newCatalogueBuilder creates a new catalogue builder with an empty catalogue. The
// default schema is set based on the engine dialect.
//
// Takes engine (dialect.DialectPort) which provides the database dialect and DDL
// interpretation.
// Takes prefixes (namespace.Set) which names the directive prefixes recognised in
// migration comments.
//
// Returns *catalogueBuilder which is ready to apply migration mutations.
func newCatalogueBuilder(engine dialect.DialectPort, prefixes namespace.Set) *catalogueBuilder {
	info := engine.Info()
	searchPath := make([]string, 0, len(info.SchemaSearchPath))
	for _, entry := range info.SchemaSearchPath {
		searchPath = append(searchPath, info.Identifiers.Fold(entry, false))
	}
	defaultSchema := ""
	if len(searchPath) > 0 {
		defaultSchema = searchPath[0]
	}

	catalogue := &sqlmodel.Catalogue{
		Schemas:       make(map[string]*sqlmodel.Schema),
		DefaultSchema: defaultSchema,
		SearchPath:    searchPath,
		Identifiers:   info.Identifiers,
		Extensions:    make(map[string]struct{}),
	}

	if defaultSchema != "" {
		catalogue.Schemas[catalogue.Key(defaultSchema)] = newEmptySchema(defaultSchema)
	}

	return &catalogueBuilder{
		engine:                  engine,
		catalogue:               catalogue,
		calledFunctions:         make(map[*sqlmodel.Function][]string),
		calledTableFunctions:    make(map[*sqlmodel.Function][]string),
		prefixes:                prefixes,
		columnOverrides:         nil,
		pendingColumnDirectives: nil,
		statementDiagnostics:    nil,
		partitionParents:        make(map[*sqlmodel.Table]*sqlmodel.Table),
		inheritances:            nil,
		localColumns:            make(map[*sqlmodel.Table]map[string]struct{}),
		statement:               statementSite{filename: "", line: 0},
		searchPaths:             newSearchPaths(searchPath),
		followingViews:          nil,
		lexicon:                 syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical},
	}
}

// ApplyMigration parses and applies all DDL statements from a single migration file to
// the catalogue.
//
// A panic in the dialect while parsing the migration or applying one of its statements is
// recovered and reported as a Q070 error, on the statement's line when it happened while
// applying one.
//
// Takes filename (string) which identifies the migration file for diagnostic messages.
// Takes content ([]byte) which holds the raw SQL content of the migration.
// Takes migrationIndex (int) which specifies the ordinal position of this migration in
// the sequence.
//
// Returns []diag.SourceError which holds any source-mapped diagnostics from parsing or
// applying the migration.
// Returns error when the context is cancelled before the migration is fully applied,
// wrapping the context's cause.
func (b *catalogueBuilder) ApplyMigration(
	ctx context.Context,
	filename string,
	content []byte,
	migrationIndex int,
) (diagnostics []diag.SourceError, err error) {
	ctx, end := observe.From(ctx).Start(ctx, observe.StageCatalogueMigration)
	defer func() { end(err) }()

	text := string(content)
	parsed, parseError := panicguard.RunValue(ctx, "catalogue: parse migration", func() (parsedMigration, error) {
		return b.parseMigration(ctx, text, filename)
	})
	if cause := context.Cause(ctx); cause != nil {
		return nil, fmt.Errorf("applying migration %s: %w", filename, cause)
	}
	if parseError != nil {
		return append(parsed.problems, diagbuild.Failure(filename, text, 1, "failed to parse migration", parseError)), nil
	}

	origin := sqlmodel.MigrationOrigin{
		Filename:  filename,
		Index:     migrationIndex,
		Statement: 0,
	}

	directives := parsed.directives
	columnDirectives := b.readColumnDirectives(directives.ColumnOverrides)
	b.pendingColumnDirectives = columnDirectives
	b.beginMigration()
	source := migrationSource{text: text, filename: filename}
	diagnostics = b.applyStatements(ctx, parsed.statements, origin, directives.ReadOnlyOverrides, source)
	b.endMigration()
	b.pendingColumnDirectives = nil
	if cause := context.Cause(ctx); cause != nil {
		return diagnostics, fmt.Errorf("applying migration %s: %w", filename, cause)
	}
	diagnostics = append(diagnostics, b.applyMigrationColumnOverrides(columnDirectives, filename)...)
	diagnostics = append(diagnostics, parsed.problems...)
	diagnostics = append(diagnostics, migrationConsistencyWarnings(directives, filename, b.lexicon.LinePrefix, b.prefixes)...)
	diagnostics = append(diagnostics, b.transactionWarnings(ctx, parsed, source)...)

	return diagnostics, nil
}

// Catalogue returns the built catalogue, each partition naming the table it partitions.
//
// Returns *sqlmodel.Catalogue which holds the accumulated schema state.
func (b *catalogueBuilder) Catalogue() *sqlmodel.Catalogue {
	b.namePartitionParents()
	return b.catalogue
}

// transactionWarnings warns when a migration of more than one statement must run outside
// a transaction, because a directive asks for it or a statement cannot run inside one,
// since the whole migration then runs without one and is not atomic.
//
// The dialect's RequiresNoTransaction predicate reads the significant tokens of each
// statement from the tokens the migration was read with once. A panic in the predicate is
// recovered and reported as a Q070 error.
//
// Takes parsed (parsedMigration) which holds the migration's statements, tokens and
// directives.
// Takes source (migrationSource) which holds the migration's text and filename.
//
// Returns []diag.SourceError which holds the warning or the error, or nil.
func (b *catalogueBuilder) transactionWarnings(ctx context.Context, parsed parsedMigration, source migrationSource) []diag.SourceError {
	if len(parsed.statements) < 2 {
		return nil
	}
	required, err := panicguard.RunValue(ctx, "catalogue: check whether the migration must run outside a transaction", func() (bool, error) {
		return parsed.directives.NoTransaction || statementsRequireNoTransaction(b.lexicon.Rules.RequiresNoTransaction, parsed.tokens, parsed.statements), nil
	})
	if err != nil {
		return []diag.SourceError{diagbuild.Failure(source.filename, source.text, 1, "failed to check whether the migration must run outside a transaction", err)}
	}
	if !required {
		return nil
	}
	return []diag.SourceError{diagbuild.Block(
		source.filename,
		1,
		diag.CodeMigrationMixedTransaction,
		diag.SeverityWarning,
		"migration mixes a non-transactional statement ("+b.prefixes.Qualify(directive.RoleMigration)+"(no_transaction: true) "+
			"or an auto-detected statement such as CREATE INDEX CONCURRENTLY) with other statements; "+
			"the whole migration runs without a transaction and is not atomic, so move the "+
			"non-transactional statement into its own migration file",
	)}
}

// parseMigration reads a migration's tokens once, its directive comments from them, and
// parses its statements with the dialect.
//
// Takes text (string) which is the migration's SQL.
// Takes filename (string) which names the migration in the directives' diagnostics.
//
// Returns parsedMigration which holds the statements, tokens and directives, and what is
// wrong with the directives; the directives are read even when the statements do not
// parse.
// Returns error when the dialect cannot parse the migration.
func (b *catalogueBuilder) parseMigration(ctx context.Context, text, filename string) (parsedMigration, error) {
	scan := syntax.ScanTokens(b.lexicon.Rules, text)
	directives, problems := syntax.ParseMigrationTokens(text, scan, b.lexicon, b.prefixes, filename)
	parsed := parsedMigration{statements: nil, tokens: scan.Tokens, problems: problems, directives: directives}
	statements, err := b.engine.ParseStatements(ctx, text)
	if err != nil {
		return parsed, err
	}
	parsed.statements = statements
	return parsed, nil
}

// applySeed adds the seed's custom functions to the catalogue and keeps its column
// overrides for the views the migrations create.
//
// Takes seed (Seed) which holds the custom functions and column overrides.
func (b *catalogueBuilder) applySeed(seed Seed) {
	MergeCustomFunctions(b.catalogue, b.engine, seed.CustomFunctions)
	b.columnOverrides = readConfiguredColumnOverrides(seed.ColumnOverrides)
}

// readColumnDirectives reads a migration's column directives once: the column each names,
// folded under the dialect's identifier rules, and its Go type and struct tags.
//
// Takes overrides ([]syntax.MigrationColumnOverride) which are the directives.
//
// Returns []columnDirective which holds one read directive per override, in order.
func (b *catalogueBuilder) readColumnDirectives(overrides []syntax.MigrationColumnOverride) []columnDirective {
	if len(overrides) == 0 {
		return nil
	}
	directives := make([]columnDirective, len(overrides))
	for index := range overrides {
		override := &overrides[index]
		read := &directives[index]
		read.override = override
		read.schema, read.table, read.column, read.named = b.columnDirectiveNames(override)
		read.tags, read.tagsError = directiveStructTags(override)
		if override.GoType != "" {
			goType, problem := directive.ParseGoTypeOverride(override.GoType)
			read.goTypeProblem = problem
			if problem == "" {
				read.goType = goType
			}
		}
	}
	return directives
}

// columnDirectiveNames folds the parts of the name a column directive gives under the
// dialect's identifier rules: a quoted part keeps its case, and an unquoted one is folded
// as the dialect folds an unquoted identifier.
//
// Takes override (*syntax.MigrationColumnOverride) which is the directive.
//
// Returns schema (string) which is the canonical schema name, or empty for none.
// Returns table (string) which is the canonical table name.
// Returns column (string) which is the canonical column name.
// Returns named (bool) which is false when the name does not read as a column of a table.
func (b *catalogueBuilder) columnDirectiveNames(override *syntax.MigrationColumnOverride) (schema, table, column string, named bool) {
	parts, split := syntax.SplitQualifiedName(override.Table + "." + override.Column)
	if !split || len(parts) < 2 {
		return "", "", "", false
	}
	names := make([]string, len(parts))
	for position, part := range parts {
		names[position] = b.catalogue.Identifiers.Fold(part.Name, part.Quoted)
	}
	if len(names) > 2 {
		schema = names[0]
	}
	return schema, names[len(names)-2], names[len(names)-1], true
}

// applyMigrationColumnOverrides applies each of a migration's column directives to the
// column it names. A directive naming no column of the catalogue produces a Q037
// diagnostic on the directive's line, with a Levenshtein suggestion drawn from the
// catalogue's table.column references as a directive writes them.
//
// Takes directives ([]columnDirective) which are the migration's column directives.
// Takes filename (string) which identifies the file for diagnostic messages.
//
// Returns []diag.SourceError which holds any override diagnostics, nil when none.
func (b *catalogueBuilder) applyMigrationColumnOverrides(directives []columnDirective, filename string) []diag.SourceError {
	if len(directives) == 0 {
		return nil
	}

	var knownColumns []string
	errorBuilder := diagbuild.NewErrorBuilder(filename)

	var diagnostics []diag.SourceError
	for index := range directives {
		read := &directives[index]
		override := read.override
		column := b.columnDirectiveTarget(read)
		if column == nil {
			if knownColumns == nil {
				knownColumns = collectKnownTableColumns(b.catalogue, b.directiveColumnName)
			}
			line := max(override.GoTypeSpan.Line, 1)
			diagnostics = append(diagnostics, errorBuilder.UnknownOverrideMigrationColumn(
				diagbuild.TextSpan{Line: line, Column: 1, EndLine: line, EndColumn: 1},
				b.prefixes.Qualify(directive.RoleColumn),
				override.Table+"."+override.Column,
				knownColumns,
			))
			continue
		}
		if read.tagsError != nil {
			diagnostics = append(diagnostics, errorBuilder.At(
				override.StructTagsSpan,
				diag.CodeInvalidKeywordArgumentValue,
				fmt.Sprintf("struct_tags for %s.%s: %s", override.Table, override.Column, read.tagsError),
			))
		}
		applyColumnDirective(column, read)
		if read.goTypeProblem != "" {
			diagnostics = append(diagnostics, errorBuilder.At(
				override.GoTypeSpan,
				diag.CodeUnusableGoTypeOverride,
				fmt.Sprintf(
					"migration go_type override %q for %s.%s is unusable: %s",
					override.GoType, override.Table, override.Column, read.goTypeProblem,
				),
			))
		}
	}
	return diagnostics
}

// applyPendingColumnOverrides sets, on every column that exists so far, the overrides the
// current migration's column directives and the configured column overrides give it, so a
// view analysed next carries them. Both were read once, so nothing is parsed here.
//
// Nothing is reported here: the directives are checked once the whole migration is
// applied, and the configured overrides once every migration is.
func (b *catalogueBuilder) applyPendingColumnOverrides() {
	for index := range b.pendingColumnDirectives {
		read := &b.pendingColumnDirectives[index]
		if column := b.columnDirectiveTarget(read); column != nil {
			applyColumnDirective(column, read)
		}
	}
	for index := range b.columnOverrides {
		configured := &b.columnOverrides[index]
		if column := findReferencedColumn(b.catalogue, configured.reference); column != nil {
			applyColumnTypeOverride(column, configured.override)
		}
	}
}

// columnDirectiveTarget finds the column a migration column directive names.
//
// Takes read (*columnDirective) which is the directive.
//
// Returns *sqlmodel.Column which aliases the catalogue's column, or nil when there is
// none.
func (b *catalogueBuilder) columnDirectiveTarget(read *columnDirective) *sqlmodel.Column {
	if !read.named {
		return nil
	}
	return b.catalogue.FindColumn(read.schema, read.table, read.column)
}

// directiveColumnName writes a canonical name as a migration column directive must for
// the dialect to fold it back to that name: as it is, or in double quotes when folding it
// as an unquoted identifier would change it or it is not a plain identifier.
//
// Takes name (string) which is a canonical name.
//
// Returns string which is the name as a directive writes it.
func (b *catalogueBuilder) directiveColumnName(name string) string {
	if b.catalogue.Identifiers.Fold(name, false) == name && isPlainIdentifier(name) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// applyStatements iterates over parsed DDL statements, interprets each one via the
// engine, and applies the resulting mutations to the catalogue.
//
// Each statement is applied under a panic guard: a panic in the dialect or the builder is
// logged once with its stack and reported as a Q070 error on the statement's line, and
// the next statement is applied.
//
// Takes statements ([]ast.ParsedStatement) which holds the parsed DDL statements.
// Takes origin (sqlmodel.MigrationOrigin) which identifies the source migration file.
// Takes readOnlyOverrides (map[int]*bool) which maps the offset of each statement that
// creates a function under a read-only directive to the directive's value.
// Takes source (migrationSource) which holds the migration's text and filename, for
// diagnostic locations.
//
// Returns []diag.SourceError which holds any diagnostics encountered during
// interpretation or application, a mutation the catalogue refuses reported on the line of
// the statement that made it. It stops early, with what it found so far, when the context
// is cancelled.
func (b *catalogueBuilder) applyStatements(
	ctx context.Context,
	statements []ast.ParsedStatement,
	origin sqlmodel.MigrationOrigin,
	readOnlyOverrides map[int]*bool,
	source migrationSource,
) []diag.SourceError {
	var diagnostics []diag.SourceError
	for position, statement := range statements {
		if ctx.Err() != nil {
			return diagnostics
		}
		b.statement = statementSite{filename: source.filename, line: source.lineOf(statement.Location)}
		origin.Statement = position

		var applied []diag.SourceError
		readOnly := readOnlyOverrides[statement.Location]
		err := panicguard.Run(ctx, "catalogue: apply migration statement", func() error {
			b.applyStatement(ctx, statement, origin, readOnly, source, &applied)
			return nil
		})
		diagnostics = append(diagnostics, applied...)
		diagnostics = append(diagnostics, b.statementDiagnostics...)
		b.statementDiagnostics = nil
		if err != nil {
			diagnostics = append(diagnostics, b.statementFailure(source, "failed to apply statement", err))
		}
	}
	return diagnostics
}

// applyStatement interprets one statement via the engine and applies its mutations, the
// primary first and then each additional one.
//
// Takes statement (ast.ParsedStatement) which is the statement.
// Takes origin (sqlmodel.MigrationOrigin) which identifies the statement's migration and
// position.
// Takes readOnly (*bool) which is the value a read-only directive gives the functions the
// statement creates, or nil when no directive precedes it.
// Takes source (migrationSource) which holds the migration's text and filename.
// Takes applied (*[]diag.SourceError) which receives the statement's diagnostics in
// order, so those found before a panic are kept.
func (b *catalogueBuilder) applyStatement(
	ctx context.Context,
	statement ast.ParsedStatement,
	origin sqlmodel.MigrationOrigin,
	readOnly *bool,
	source migrationSource,
	applied *[]diag.SourceError,
) {
	mutation, ddlError := b.engine.ApplyDDL(ctx, statement)
	if ddlError != nil {
		if ctx.Err() == nil {
			*applied = append(*applied, b.statementFailure(source, "failed to interpret DDL", ddlError))
		}
		return
	}
	if mutation == nil {
		return
	}

	mutations := append([]*ast.CatalogueMutation{mutation}, mutation.AdditionalMutations...)
	mutation.AdditionalMutations = nil
	for _, next := range mutations {
		if ctx.Err() != nil {
			return
		}
		if next == nil {
			continue
		}
		next.Origin = origin
		applyMigrationReadOnlyOverride(next, readOnly)
		*applied = append(*applied, b.unanalysedParts(next, source)...)

		if mutationError := b.applyMutation(ctx, next); mutationError != nil {
			*applied = append(*applied, diagbuild.Block(
				source.filename,
				max(b.statement.line, 1),
				mutationErrorCode(mutationError),
				diag.SeverityError,
				mutationError.Error(),
			))
		}
		*applied = append(*applied, b.statementDiagnostics...)
		b.statementDiagnostics = nil
	}
}

// statementFailure reports a dialect's error on the statement being applied: at the place
// in the migration the error locates, or on the statement's line when it locates none. A
// recovered panic is reported as Q070 and anything else as Q010.
//
// Takes source (migrationSource) which holds the migration's text and filename.
// Takes failed (string) which says what failed.
// Takes err (error) which is the error.
//
// Returns diag.SourceError which is the error diagnostic.
func (b *catalogueBuilder) statementFailure(source migrationSource, failed string, err error) diag.SourceError {
	failure := diagbuild.Failure(source.filename, source.text, 1, failed, err)
	if failure.EndLine == 0 {
		failure.Line = max(b.statement.line, 1)
	}
	return failure
}

// unanalysedParts reports, as Q069 warnings, the parts of a statement the dialect could
// not analyse: each at its own place in the migration when the dialect located it, and on
// the statement's line otherwise.
//
// Takes mutation (*ast.CatalogueMutation) which lists the parts.
// Takes source (migrationSource) which holds the migration's text and filename.
//
// Returns []diag.SourceError which holds one warning per part.
func (b *catalogueBuilder) unanalysedParts(mutation *ast.CatalogueMutation, source migrationSource) []diag.SourceError {
	if len(mutation.UnanalysedParts) == 0 {
		return nil
	}
	diagnostics := make([]diag.SourceError, 0, len(mutation.UnanalysedParts))
	for _, part := range mutation.UnanalysedParts {
		span := diagbuild.TextSpan{Line: max(b.statement.line, 1), Column: 1, EndLine: 0, EndColumn: 0}
		if part.Span.Known() && part.Span.Start >= 0 && part.Span.End <= len(source.text) {
			span = diagbuild.SpanOf(source.text, 1, part.Span.Start, part.Span.End)
		}
		diagnostics = append(diagnostics, diagbuild.Located(source.filename, span, diag.CodePartlyAnalysed,
			diag.SeverityWarning, part.Message, ""))
	}
	return diagnostics
}

// applyMutation dispatches a catalogue mutation and updates affected partitions and
// dependent views. IF EXISTS mutations targeting a missing object or column leave the
// catalogue unchanged.
//
// Takes mutation (*ast.CatalogueMutation) which holds the mutation to apply.
//
// Returns error when the mutation handler fails or the kind is unknown.
func (b *catalogueBuilder) applyMutation(ctx context.Context, mutation *ast.CatalogueMutation) error {
	if b.skipsMissingTarget(mutation) {
		return nil
	}
	err := b.dispatchMutation(ctx, mutation)
	if mutation.Kind == ast.MutationDropSchema && len(b.partitionParents) > 0 {
		b.dropOrphanedPartitions()
	}
	if err == nil && len(b.followingViews) > 0 && changesRelationColumns(mutation.Kind) {
		b.deriveFollowingViews(ctx, b.changedRelations(mutation))
	}
	return err
}

// dispatchMutation dispatches a single catalogue mutation to the appropriate handler
// based on its kind.
//
// Takes mutation (*ast.CatalogueMutation) which holds the mutation to apply.
//
// Returns error when the mutation handler fails or the kind is unknown.
func (b *catalogueBuilder) dispatchMutation(ctx context.Context, mutation *ast.CatalogueMutation) error {
	switch mutation.Kind {
	case ast.MutationCreateTable:
		return b.applyCreateTable(ctx, mutation)
	case ast.MutationCreateFunction:
		return b.applyCreateFunction(ctx, mutation)
	case ast.MutationCreateView:
		return b.applyCreateView(ctx, mutation)
	default:
		if handler, ok := mutationHandlers[mutation.Kind]; ok {
			return handler(b, mutation)
		}
		return fmt.Errorf("unknown mutation kind: %d", mutation.Kind)
	}
}

// resolveCustomColumnType resolves unknown column types against catalogue definitions,
// the column's own type and every type nested in it: a list's elements, a map's keys and
// values, a struct's fields and a union's members.
//
// It honours schemas and the search path. Domains contribute their underlying type and
// nullability constraints.
//
// Takes column (*sqlmodel.Column) which holds the column whose type to resolve.
func (b *catalogueBuilder) resolveCustomColumnType(column *sqlmodel.Column) {
	b.resolveColumnTypeName(column)
	column.SQLType = b.resolveNestedTypeNames(column.SQLType)
}

// resolveNestedTypeNames resolves the unknown types nested in a type against the
// catalogue's enums, composite types and domains, leaving each nested type's nullability
// as declared. The nested types are copied, so a type shared with another column or the
// mutation that declared it is not changed.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns sqlmodel.SQLType which is the type with its nested types resolved.
func (b *catalogueBuilder) resolveNestedTypeNames(sqlType sqlmodel.SQLType) sqlmodel.SQLType {
	return b.resolveNestedTypeNamesAt(sqlType, 0)
}

// resolveNestedTypeNamesAt resolves the unknown types nested in a type that is itself
// nested depth levels deep, going no deeper than sqlmodel.MaxTypeDepth levels.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
// Takes depth (int) which is how deeply the type is nested.
//
// Returns sqlmodel.SQLType which is the type with its nested types resolved.
func (b *catalogueBuilder) resolveNestedTypeNamesAt(sqlType sqlmodel.SQLType, depth int) sqlmodel.SQLType {
	if depth+1 >= sqlmodel.MaxTypeDepth {
		return sqlType
	}
	if sqlType.ElementType != nil {
		sqlType.ElementType = new(b.resolveNestedTypeName(*sqlType.ElementType, depth+1))
	}
	if sqlType.KeyType != nil {
		sqlType.KeyType = new(b.resolveNestedTypeName(*sqlType.KeyType, depth+1))
	}
	if len(sqlType.StructFields) > 0 {
		fields := slices.Clone(sqlType.StructFields)
		for index := range fields {
			fields[index].SQLType = b.resolveNestedTypeName(fields[index].SQLType, depth+1)
		}
		sqlType.StructFields = fields
	}
	if len(sqlType.UnionMembers) > 0 {
		members := slices.Clone(sqlType.UnionMembers)
		for index := range members {
			members[index].SQLType = b.resolveNestedTypeName(members[index].SQLType, depth+1)
		}
		sqlType.UnionMembers = members
	}
	return sqlType
}

// resolveNestedTypeName resolves a type nested in another, and the types nested in it,
// keeping its nullability as declared.
//
// Takes sqlType (sqlmodel.SQLType) which is the nested type.
// Takes depth (int) which is how deeply it is nested.
//
// Returns sqlmodel.SQLType which is the resolved type.
func (b *catalogueBuilder) resolveNestedTypeName(sqlType sqlmodel.SQLType, depth int) sqlmodel.SQLType {
	if sqlType.Category == sqlmodel.TypeCategoryUnknown && sqlType.EngineName != "" {
		nullable := sqlType.Nullable
		sqlType = b.resolveNamedType(sqlType)
		sqlType.Nullable = nullable
	}
	return b.resolveNestedTypeNamesAt(sqlType, depth)
}

// resolveNamedType resolves a type by name against the catalogue's domains, enums and
// composite types, as sqlmodel.Catalogue.ResolveNamedType does, and copies the result so
// it shares no values, such as an enum's, with the type it names.
//
// Takes sqlType (sqlmodel.SQLType) which is the type to resolve.
//
// Returns sqlmodel.SQLType which is the resolved copy.
func (b *catalogueBuilder) resolveNamedType(sqlType sqlmodel.SQLType) sqlmodel.SQLType {
	return cloneSQLType(b.catalogue.ResolveNamedType(sqlType))
}

// resolveColumnTypeName resolves a column's own type when it is unknown: a domain gives
// its underlying type, array dimensions and NOT NULL constraint, and an enum or composite
// type its own type.
//
// Takes column (*sqlmodel.Column) which holds the column whose type to resolve.
func (b *catalogueBuilder) resolveColumnTypeName(column *sqlmodel.Column) {
	if column.SQLType.Category != sqlmodel.TypeCategoryUnknown || column.SQLType.EngineName == "" {
		return
	}
	if domain := b.catalogue.LookupDomain(column.SQLType.Schema, column.SQLType.EngineName); domain != nil {
		if domain.NotNull && column.ArrayDimensions == 0 {
			column.Nullable = false
		}
		column.SQLType = cloneSQLType(domain.BaseType)
		column.ArrayDimensions += domain.ArrayDimensions
		column.IsArray = column.ArrayDimensions > 0
		return
	}
	column.SQLType = b.resolveNamedType(column.SQLType)
}

// key returns the key the catalogue files a canonical name under, following the dialect's
// identifier rules.
//
// Takes name (string) which is a canonical name.
//
// Returns string which is the map key.
func (b *catalogueBuilder) key(name string) string {
	return b.catalogue.Key(name)
}

// sameName reports whether two canonical names name the same object under the dialect's
// identifier rules.
//
// Takes left (string) which is the first name.
// Takes right (string) which is the second name.
//
// Returns bool which is true when the names match.
func (b *catalogueBuilder) sameName(left, right string) bool {
	return b.catalogue.Identifiers.Equal(left, right)
}

// resolveSchema returns the schema for the given name, creating it if it does not exist.
// An empty name is treated as the schema unqualified objects are created in, the first on
// the search path.
//
// Takes schemaName (string) which specifies the schema name to resolve.
//
// Returns *sqlmodel.Schema which is the resolved or newly created schema.
func (b *catalogueBuilder) resolveSchema(schemaName string) *sqlmodel.Schema {
	if schemaName == "" {
		schemaName = b.creationSchema()
	}
	schema, exists := b.catalogue.Schemas[b.key(schemaName)]
	if !exists {
		schema = newEmptySchema(schemaName)
		b.catalogue.Schemas[b.key(schemaName)] = schema
	}
	return schema
}

// findTable looks up a table by schema and name, returning an error if not found.
//
// An unqualified name is looked up along the search path. The lookup is read-only and
// never creates a schema.
//
// Takes schemaName (string) which specifies the schema to search in, or empty.
// Takes tableName (string) which specifies the table to find.
//
// Returns *sqlmodel.Table which is the found table.
// Returns error when the table does not exist.
func (b *catalogueBuilder) findTable(schemaName string, tableName string) (*sqlmodel.Table, error) {
	if table := b.catalogue.LookupTable(schemaName, tableName); table != nil {
		return table, nil
	}
	return nil, unknownObject(tableNotFoundFormat, tableName, b.searchedSchemas(schemaName))
}

// statementSite locates a migration statement for the diagnostics found while applying
// it.
type statementSite struct {
	// filename is the migration file.
	filename string

	// line is the 1-based line of the statement's first significant token.
	line int
}

// parsedMigration is a migration's text as the dialect parsed it, with what its directive
// comments say.
type parsedMigration struct {
	// statements are the migration's statements, in order.
	statements []ast.ParsedStatement

	// tokens are the migration's tokens under the dialect's lexical rules, read once for the
	// directives and the transaction check.
	tokens []sqllex.Token

	// problems holds what is wrong with the migration's directive comments.
	problems []diag.SourceError

	// directives holds what the migration's directive comments say.
	directives syntax.MigrationDirectives
}

// columnDirective is a migration column directive read once for the migration that holds
// it: the column it names, folded under the dialect's identifier rules, and its Go type
// and struct tags parsed.
type columnDirective struct {
	// override is the directive as the migration writes it.
	override *syntax.MigrationColumnOverride

	// goType is the parsed go_type, or nil when the directive gives none or it is unusable.
	goType *sqlmodel.GoType

	// tags holds the parsed struct tags, or nil when the directive gives none or they do not
	// parse.
	tags map[string]string

	// tagsError is why the struct tags do not parse, or nil.
	tagsError error

	// goTypeProblem describes why the go_type is unusable, or is empty.
	goTypeProblem string

	// schema is the canonical name of the schema the directive names, or empty for none.
	schema string

	// table is the canonical name of the table the directive names.
	table string

	// column is the canonical name of the column the directive names.
	column string

	// named reports whether the directive's name reads as a column of a table.
	named bool
}

// configuredColumnOverride is a configured type override that names a column, read once:
// its column reference split and its Go type found usable.
type configuredColumnOverride struct {
	// reference names the column.
	reference ColumnReference

	// override is the configured override.
	override sqlmodel.TypeOverride
}

// migrationSource is the text of the migration being applied and the file it came from.
type migrationSource struct {
	// text is the migration's SQL.
	text string

	// filename identifies the migration file.
	filename string
}

// lineOf returns the 1-based line of a byte offset into the migration's text.
//
// Takes offset (int) which is the byte offset.
//
// Returns int which is the line.
func (source migrationSource) lineOf(offset int) int {
	offset = max(0, min(offset, len(source.text)))
	return 1 + strings.Count(source.text[:offset], "\n")
}

// migrationConsistencyWarnings reports the query headers and the directives with a
// foreign prefix that a migration holds.
//
// Takes directives (syntax.MigrationDirectives) which holds what the migration's
// directive comments say.
// Takes filename (string) which identifies the migration file.
// Takes linePrefix (string) which opens a directive comment in the dialect.
// Takes prefixes (namespace.Set) which names the directive prefixes to recognise.
//
// Returns []diag.SourceError which holds the warnings, or nil when there are none.
func migrationConsistencyWarnings(directives syntax.MigrationDirectives, filename, linePrefix string, prefixes namespace.Set) []diag.SourceError {
	diagnostics := make([]diag.SourceError, 0, len(directives.MisplacedQueryLines)+len(directives.Foreign))
	for _, lineNumber := range directives.MisplacedQueryLines {
		diagnostics = append(diagnostics, diagbuild.Block(
			filename,
			lineNumber,
			diag.CodeDirectiveWrongContext,
			diag.SeverityWarning,
			prefixes.Qualify(directive.RoleQuery)+" is a query directive and is ignored in a migration file; use "+
				prefixes.Qualify(directive.RoleMigration)+" for migration directives",
		))
	}

	errorBuilder := diagbuild.NewErrorBuilder(filename)
	for _, foreign := range directives.Foreign {
		span := diagbuild.TextSpan{Line: foreign.Line, Column: 1, EndLine: foreign.Line, EndColumn: 1}
		diagnostics = append(diagnostics, errorBuilder.ForeignPrefix(span, linePrefix, foreign.Name, foreign.Prefix))
	}
	return diagnostics
}

// collectKnownTableColumns walks every schema's tables in the catalogue and produces the
// full list of table.column references, each name written by render. The result serves as
// the Levenshtein candidate set for Q037.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schemas to walk.
// Takes render (func(string) string) which writes a table or column name as the
// suggestion shows it.
//
// Returns []string which is the sorted list of table.column references.
func collectKnownTableColumns(catalogue *sqlmodel.Catalogue, render func(string) string) []string {
	if catalogue == nil {
		return nil
	}
	var known []string
	for _, schema := range catalogue.Schemas {
		if schema == nil {
			continue
		}
		for _, table := range schema.Tables {
			if table == nil {
				continue
			}
			for columnIndex := range table.Columns {
				known = append(known, render(table.Name)+"."+render(table.Columns[columnIndex].Name))
			}
		}
	}

	slices.Sort(known)
	return known
}

// applyColumnDirective sets what a migration column directive declares on its column, its
// struct tags over those already there key by key. A go_type the generator cannot write
// is not set.
//
// Takes column (*sqlmodel.Column) which receives the overrides.
// Takes read (*columnDirective) which is the directive, read once.
func applyColumnDirective(column *sqlmodel.Column, read *columnDirective) {
	override := read.override
	if override.SQLType != "" {
		column.SQLTypeOverride = override.SQLType
	}
	if override.Nullable != nil {
		column.NullableOverride = new(*override.Nullable)
	}
	if override.NonNullElements != nil {
		column.NonNullElements = new(*override.NonNullElements)
	}
	if len(read.tags) > 0 {
		column.StructTags = mergeStructTags(column.StructTags, read.tags, true)
	}
	if read.goType != nil {
		column.GoTypeOverride = cloneGoType(read.goType)
	}
}

// directiveStructTags reads the struct tags a migration column directive declares.
//
// Takes override (*syntax.MigrationColumnOverride) which is the directive.
//
// Returns map[string]string which holds the tags, or nil when the directive declares
// none.
// Returns error when the struct_tags value is not a struct tag a generated field can
// carry.
func directiveStructTags(override *syntax.MigrationColumnOverride) (map[string]string, error) {
	if override.StructTags == "" {
		return nil, nil
	}
	return structtag.Parse(override.StructTags)
}

// catalogueMutationNoOp is the shared handler for mutation kinds that the catalogue
// builder deliberately ignores. Centralising the function pointer keeps mutationHandlers
// compact and makes the intent of the no-op entries obvious at the call site.
//
// Its builder and mutation arguments are unused.
//
// Returns error which is always nil.
func catalogueMutationNoOp(_ *catalogueBuilder, _ *ast.CatalogueMutation) error {
	return nil
}

func init() {
	mutationHandlers = map[ast.MutationKind]func(*catalogueBuilder, *ast.CatalogueMutation) error{
		ast.MutationDropTable:                (*catalogueBuilder).applyDropTable,
		ast.MutationAlterTableAddColumn:      (*catalogueBuilder).applyAlterTableAddColumn,
		ast.MutationAlterTableDropColumn:     (*catalogueBuilder).applyAlterTableDropColumn,
		ast.MutationAlterTableAlterColumn:    (*catalogueBuilder).applyAlterTableAlterColumn,
		ast.MutationAlterTableRenameColumn:   (*catalogueBuilder).applyAlterTableRenameColumn,
		ast.MutationAlterTableRenameTable:    (*catalogueBuilder).applyAlterTableRenameTable,
		ast.MutationAlterTableSetSchema:      (*catalogueBuilder).applyAlterTableSetSchema,
		ast.MutationCreateEnum:               (*catalogueBuilder).applyCreateEnum,
		ast.MutationAlterEnumAddValue:        (*catalogueBuilder).applyAlterEnumAddValue,
		ast.MutationAlterEnumRenameValue:     (*catalogueBuilder).applyAlterEnumRenameValue,
		ast.MutationDropEnum:                 (*catalogueBuilder).applyDropEnum,
		ast.MutationCreateCompositeType:      (*catalogueBuilder).applyCreateCompositeType,
		ast.MutationCreateDomain:             (*catalogueBuilder).applyCreateDomain,
		ast.MutationDropType:                 (*catalogueBuilder).applyDropType,
		ast.MutationDropFunction:             (*catalogueBuilder).applyDropFunction,
		ast.MutationCreateSchema:             (*catalogueBuilder).applyCreateSchema,
		ast.MutationDropSchema:               (*catalogueBuilder).applyDropSchema,
		ast.MutationDropView:                 (*catalogueBuilder).applyDropView,
		ast.MutationCreateIndex:              (*catalogueBuilder).applyCreateIndex,
		ast.MutationDropIndex:                (*catalogueBuilder).applyDropIndex,
		ast.MutationCreateExtension:          (*catalogueBuilder).applyCreateExtension,
		ast.MutationComment:                  (*catalogueBuilder).applyComment,
		ast.MutationAlterTableAddConstraint:  (*catalogueBuilder).applyAlterTableAddConstraint,
		ast.MutationAlterTableDropConstraint: (*catalogueBuilder).applyAlterTableDropConstraint,
		ast.MutationCreateSequence:           (*catalogueBuilder).applyCreateSequence,
		ast.MutationDropSequence:             (*catalogueBuilder).applyDropSequence,
		ast.MutationAttachPartition:          (*catalogueBuilder).applyAttachPartition,
		ast.MutationDetachPartition:          (*catalogueBuilder).applyDetachPartition,
		ast.MutationAlterTypeRename:          (*catalogueBuilder).applyAlterTypeRename,
		ast.MutationAlterTypeSetSchema:       (*catalogueBuilder).applyAlterTypeSetSchema,
		ast.MutationSetSearchPath:            (*catalogueBuilder).applySetSearchPath,
		ast.MutationResetSearchPath:          (*catalogueBuilder).applyResetSearchPath,
	}
	for _, kind := range noOpMutations {
		mutationHandlers[kind] = catalogueMutationNoOp
	}
}

// mutationErrorCode returns the code a mutation the catalogue refuses is reported with:
// Q067 for an object that already exists, Q068 for one that does not, Q070 for a panic
// recovered from the dialect, and Q010 for anything else.
//
// Takes err (error) which is the mutation's error.
//
// Returns string which is the diagnostic code.
func mutationErrorCode(err error) string {
	switch {
	case errors.Is(err, errDuplicateObject):
		return diag.CodeDuplicateObject
	case errors.Is(err, errUnknownObject):
		return diag.CodeUnknownObject
	case errors.Is(err, fault.ErrPanic):
		return diag.CodeRecoveredPanic
	default:
		return diag.CodeParseError
	}
}

// newEmptySchema creates a new schema with all map fields initialised to empty maps.
//
// Takes name (string) which specifies the schema name.
//
// Returns *sqlmodel.Schema which is the initialised empty schema.
func newEmptySchema(name string) *sqlmodel.Schema {
	return &sqlmodel.Schema{
		Name:           name,
		Tables:         make(map[string]*sqlmodel.Table),
		Views:          make(map[string]*sqlmodel.View),
		Enums:          make(map[string]*sqlmodel.Enum),
		Functions:      make(map[string][]*sqlmodel.Function),
		CompositeTypes: make(map[string]*sqlmodel.CompositeType),
		Sequences:      make(map[string]*sqlmodel.Sequence),
		Domains:        make(map[string]*sqlmodel.Domain),
	}
}

// applyMigrationReadOnlyOverride sets the data access level of a CREATE FUNCTION mutation
// made by a statement a read-only directive precedes.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation to update.
// Takes readOnly (*bool) which is the directive's value, or nil when no directive
// precedes the statement.
func applyMigrationReadOnlyOverride(mutation *ast.CatalogueMutation, readOnly *bool) {
	if readOnly == nil || mutation.Kind != ast.MutationCreateFunction {
		return
	}
	if mutation.Function == nil || mutation.Function.Function == nil {
		return
	}
	if *readOnly {
		mutation.Function.DataAccess = sqlmodel.DataAccessReadOnly
	} else {
		mutation.Function.DataAccess = sqlmodel.DataAccessModifiesData
	}
}

// readConfiguredColumnOverrides reads, once, the configured type overrides that name a
// column whose Go type the generator can write and whose reference splits; the rest are
// reported once every migration is applied.
//
// Takes overrides ([]sqlmodel.TypeOverride) which are the configured overrides.
//
// Returns []configuredColumnOverride which holds the usable column overrides, in order.
func readConfiguredColumnOverrides(overrides []sqlmodel.TypeOverride) []configuredColumnOverride {
	var configured []configuredColumnOverride
	for _, override := range overrides {
		if override.Column == "" {
			continue
		}
		if _, unusable := unusableTypeOverride(override); unusable {
			continue
		}
		reference, err := ParseColumnReference(override.Column)
		if err != nil {
			continue
		}
		configured = append(configured, configuredColumnOverride{reference: reference, override: override})
	}
	return configured
}

// statementsRequireNoTransaction reports whether any statement of a migration cannot run
// inside a transaction, by the dialect's predicate over the significant tokens of each
// statement, read from the tokens the whole migration was read with.
//
// Takes predicate (sqllex.StatementPredicate) which is the dialect's predicate; nil
// imposes no restriction.
// Takes tokens ([]sqllex.Token) which are the migration's tokens, in order.
// Takes statements ([]ast.ParsedStatement) which locate the migration's statements, in
// order.
//
// Returns bool which is true when the predicate matches a statement.
func statementsRequireNoTransaction(predicate sqllex.StatementPredicate, tokens []sqllex.Token, statements []ast.ParsedStatement) bool {
	if predicate == nil {
		return false
	}
	next := 0
	for _, statement := range statements {
		for next < len(tokens) && tokens[next].Start < statement.Location {
			next++
		}
		end := statement.Location + statement.Length
		var significant []sqllex.Token
		for ; next < len(tokens) && tokens[next].Start < end; next++ {
			if !tokens[next].Kind.IsNoise() {
				significant = append(significant, tokens[next])
			}
		}
		if predicate(significant) {
			return true
		}
	}
	return false
}

// isPlainIdentifier reports whether a name is a letter or underscore followed by letters,
// digits and underscores, which an identifier may be written as unquoted.
//
// Takes name (string) which is the name.
//
// Returns bool which is true for a plain identifier.
func isPlainIdentifier(name string) bool {
	for index, character := range name {
		if character == '_' || unicode.IsLetter(character) || (index > 0 && unicode.IsDigit(character)) {
			continue
		}
		return false
	}
	return name != ""
}
