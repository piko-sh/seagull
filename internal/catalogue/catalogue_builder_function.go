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
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxBodyStatements bounds how many statements of a SQL function body are analysed one
	// by one; a longer body is classified by a scan of its text.
	maxBodyStatements = 256
)

var (
	// nonSQLBodyKeywords holds the control-flow and SQL keywords that may appear immediately
	// before a parenthesis in a function body but are not function calls. Excluding them
	// keeps bodyScanner.calledFunctions from recording control structures as callees and
	// seeding the data-access call graph with noise.
	nonSQLBodyKeywords = map[string]struct{}{
		"if": {}, "elsif": {}, "elseif": {}, "while": {}, "for": {}, "case": {}, "when": {},
		"and": {}, "or": {}, "not": {}, "in": {}, "exists": {}, "values": {}, "select": {},
		"return": {}, "loop": {}, "raise": {}, "foreach": {}, "array": {}, "row": {}, "with": {},
		"using": {}, "on": {}, "as": {}, "then": {}, "else": {}, "over": {}, "filter": {},
		"any": {}, "all": {}, "some": {}, "distinct": {}, "from": {}, "join": {}, "lateral": {},
		"where": {}, "by": {}, "group": {}, "like": {}, "between": {}, "set": {}, "limit": {},
		"offset": {}, "perform": {}, "is": {}, "coalesce": {}, "nullif": {}, "greatest": {},
		"least": {}, "cast": {}, "extract": {}, "position": {}, "substring": {}, "overlay": {},
	}

	// relationKeywords holds the words after which a name followed by a parenthesis names a
	// relation and its columns, as in INSERT INTO audit (note), rather than a call.
	relationKeywords = map[string]struct{}{
		"into": {}, "table": {}, "references": {},
	}

	// dataChangeKeywords holds the statement keywords that change data or schema, so a body
	// holding any of them as a word, outside its strings and comments, writes.
	dataChangeKeywords = map[string]struct{}{
		"insert": {}, "update": {}, "delete": {}, "truncate": {}, "merge": {},
		"create": {}, "drop": {}, "alter": {},
	}

	// statementKeywords holds the keywords that start a statement which changes data, the
	// schema or the session, or runs SQL a scan cannot read, as EXECUTE of dynamic SQL does.
	//
	// Keywords such as comment, lock, call and copy can also be column names. They count
	// only at the start of a statement with the required following word, matched without
	// regard to case. Each map value names that word; an empty value allows any operand.
	statementKeywords = map[string]string{
		"execute": "", "call": "", "copy": "", "refresh": "MATERIALIZED", "lock": "", "notify": "",
		"grant": "", "revoke": "", "vacuum": "", "reindex": "", "cluster": "", "import": "FOREIGN",
		"comment": "ON", "security": "LABEL",
	}

	// standaloneStatementKeywords holds the statement keywords whose statement may end right
	// after them, as VACUUM; does.
	standaloneStatementKeywords = map[string]struct{}{
		"vacuum": {}, "reindex": {}, "cluster": {},
	}

	// statementOpeners holds the words after which a procedural body starts a statement:
	// those that open a block, a branch or the body of a loop.
	statementOpeners = map[string]struct{}{
		"begin": {}, "then": {}, "else": {}, "loop": {}, "do": {}, "repeat": {},
	}

	// dynamicStatementOpeners holds the words after which EXECUTE runs dynamic SQL inside
	// another statement.
	//
	// Examples include RETURN QUERY EXECUTE, FOR ... IN EXECUTE and OPEN ... FOR EXECUTE.
	dynamicStatementOpeners = map[string]struct{}{
		"query": {}, "in": {}, "for": {},
	}

	// expressionContinuations holds the words that, after a word, show the word is an
	// operand in an expression, such as a column in a CASE branch, rather than a statement
	// keyword.
	expressionContinuations = map[string]struct{}{
		"end": {}, "else": {}, "elsif": {}, "elseif": {}, "when": {}, "then": {}, "from": {}, "as": {},
		"is": {}, "and": {}, "or": {}, "not": {}, "in": {}, "like": {}, "between": {},
		"collate": {},
	}
)

// bodyScanner reads a function body with the dialect's lexical rules: the names the body
// calls and whether it changes data. Strings, comments and quoted identifiers are told
// apart from code, so a word inside a string literal or a comment is never taken for a
// call or a statement.
type bodyScanner struct {
	// builtinTypes holds the dialect's built-in type names, so a type with a modifier, such
	// as numeric(10, 2) in a declaration, is not taken for a call.
	builtinTypes *ast.TypeCatalogue

	// builtinFunctions holds the dialect's built-in functions; a name that is both a type
	// and a function is still a call.
	builtinFunctions *ast.FunctionCatalogue

	// wordOperators holds the dialect's further comparison operators that are spelt as a
	// word, such as ILIKE, in lower case: after a statement keyword they show the keyword is
	// an operand, as the expression continuations do.
	wordOperators map[string]struct{}

	// rules is how the dialect writes SQL.
	rules sqllex.Rules

	// identifiers is how the dialect folds a name written without quotes, so a called name
	// is recorded as the dialect reads it.
	identifiers sqlmodel.IdentifierCase
}

// newBodyScanner returns the body scanner for a dialect.
//
// Takes engine (dialect.DialectPort) which supplies the lexical rules and the built-in
// types and functions.
//
// Returns bodyScanner which is ready to scan bodies.
func newBodyScanner(engine dialect.DialectPort) bodyScanner {
	info := engine.Info()
	return bodyScanner{
		builtinTypes:     engine.BuiltinTypes(),
		builtinFunctions: engine.BuiltinFunctions(),
		wordOperators:    wordOperatorsOf(info.Filters.Operators),
		rules:            info.Lexical,
		identifiers:      info.Identifiers,
	}
}

// calledFunctions extracts the names of the functions a body calls, as the dialect reads
// them: a word folded as the dialect folds an unquoted name, and a quoted name as
// written.
//
// The names let the data-access call graph flow a writing callee's classification to the
// caller. A plpgsql body that mutates indirectly (for example PERFORM write_audit()) is
// otherwise scanned only for data-changing keywords and so is wrongly classified
// read-only and routed to a read replica.
//
// A call is a name, optionally qualified by dots, followed by an opening parenthesis. A
// control-flow or SQL keyword, a relation named after INTO, TABLE or REFERENCES, and a
// built-in type name that is not also a built-in function are not calls. A qualified call
// (audit.write_event(...)) is recorded under its dotted form to match the signature
// index. Over-recording a name that is not a known function is harmless when the name
// resolves to nothing that writes.
//
// Takes body (string) which holds the function body text to scan.
//
// Returns []string which holds the unique called-function names, sorted, or nil if none.
func (scanner bodyScanner) calledFunctions(body string) []string {
	tokens := sqllex.SignificantTokens(scanner.rules, body)
	seen := make(map[string]struct{})
	for index := 0; index < len(tokens); {
		name, next, ok := scanner.calledNameAt(tokens, index)
		index = next
		if ok {
			seen[name] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(seen))
}

// calledNameAt reads, starting at index, a possibly qualified name followed by an opening
// parenthesis that is a call.
//
// Takes tokens ([]sqllex.Token) which are the body's significant tokens.
// Takes index (int) which is the position to begin reading from.
//
// Returns string which is the callee name, as the dialect reads it, when ok is true.
// Returns int which is the position to continue scanning from.
// Returns bool which is true when a callee name was recognised.
func (scanner bodyScanner) calledNameAt(tokens []sqllex.Token, index int) (string, int, bool) {
	if !isNameToken(tokens[index]) {
		return "", index + 1, false
	}
	start := index
	parts := []string{scanner.nameText(tokens[index])}
	index++
	for index+1 < len(tokens) && tokens[index].Raw == "." && isNameToken(tokens[index+1]) {
		parts = append(parts, scanner.nameText(tokens[index+1]))
		index += 2
	}
	if index >= len(tokens) || tokens[index].Raw != "(" {
		return "", index, false
	}
	if start > 0 && tokens[start-1].Kind == sqllex.KindWord {
		if _, relation := relationKeywords[strings.ToLower(tokens[start-1].Raw)]; relation {
			return "", index, false
		}
	}
	name := strings.Join(parts, ".")
	if len(parts) == 1 && !scanner.isCall(tokens[start], strings.ToLower(name)) {
		return "", index, false
	}
	return name, index, true
}

// isCall reports whether an unqualified name followed by a parenthesis is a call rather
// than a keyword or a type modifier.
//
// Takes token (sqllex.Token) which is the name's token.
// Takes name (string) which is the lower-case name.
//
// Returns bool which is true when the name is a call.
func (scanner bodyScanner) isCall(token sqllex.Token, name string) bool {
	if token.Kind != sqllex.KindWord {
		return true
	}
	if _, keyword := nonSQLBodyKeywords[name]; keyword {
		return false
	}
	if scanner.builtinTypes == nil {
		return true
	}
	if _, isType := scanner.builtinTypes.Types[name]; !isType {
		return true
	}
	if scanner.builtinFunctions == nil {
		return false
	}
	_, isFunction := scanner.builtinFunctions.Functions[name]
	return isFunction
}

// nameText returns the name a token spells as the dialect reads it: a word folded as the
// dialect folds an unquoted name, or a quoted identifier as written between its quotes,
// with each doubled quote read as one.
//
// Takes token (sqllex.Token) which is a word or quoted identifier.
//
// Returns string which is the name.
func (scanner bodyScanner) nameText(token sqllex.Token) string {
	if token.Kind == sqllex.KindQuotedIdentifier && len(token.Raw) >= 2 {
		return quotedIdentifierText(token.Raw)
	}
	return scanner.identifiers.Fold(token.Raw, false)
}

// dataAccess classifies a body by whether it changes data: whether it holds a
// data-changing statement keyword as a word outside its strings and comments, or starts a
// statement with another keyword that writes or runs SQL the scan cannot read, such as
// EXECUTE, CALL or COPY.
//
// Takes body (string) which holds the function body text to scan.
//
// Returns sqlmodel.FunctionDataAccess which is DataAccessModifiesData when the body
// writes, or DataAccessReadOnly otherwise.
func (scanner bodyScanner) dataAccess(body string) sqlmodel.FunctionDataAccess {
	tokens := sqllex.SignificantTokens(scanner.rules, body)
	for index, token := range tokens {
		if token.Kind != sqllex.KindWord {
			continue
		}
		word := strings.ToLower(token.Raw)
		if _, changes := dataChangeKeywords[word]; changes || scanner.startsWritingStatement(tokens, index, word) {
			return sqlmodel.DataAccessModifiesData
		}
	}
	return sqlmodel.DataAccessReadOnly
}

// startsWritingStatement reports whether a word starts a statement that writes or runs
// SQL a scan cannot read: whether it is a statement keyword, where a statement starts,
// and followed by what that statement needs.
//
// Takes tokens ([]sqllex.Token) which are the body's significant tokens.
// Takes index (int) which is the word's position.
// Takes word (string) which is the word, in lower case.
//
// Returns bool which is true when the word starts such a statement.
func (scanner bodyScanner) startsWritingStatement(tokens []sqllex.Token, index int, word string) bool {
	follower, isStatementKeyword := statementKeywords[word]
	if !isStatementKeyword {
		return false
	}
	if !atStatementStart(tokens, index) && (word != "execute" || !followsWord(tokens, index, dynamicStatementOpeners)) {
		return false
	}
	if index+1 >= len(tokens) || tokens[index+1].Raw == ";" {
		_, standalone := standaloneStatementKeywords[word]
		return standalone
	}
	next := tokens[index+1]
	if follower != "" {
		return next.Kind == sqllex.KindWord && strings.EqualFold(next.Raw, follower)
	}
	return scanner.beginsOperand(next)
}

// beginsOperand reports whether a token can begin what a statement keyword acts on: a
// name, a string, a parameter or an opening parenthesis, but not an operator, the
// dialect's word operators included, or a word that continues an expression.
//
// Takes token (sqllex.Token) which is the token after the keyword.
//
// Returns bool which is true when the token can begin the statement's operand.
func (scanner bodyScanner) beginsOperand(token sqllex.Token) bool {
	switch token.Kind {
	case sqllex.KindWord:
		word := strings.ToLower(token.Raw)
		_, continues := expressionContinuations[word]
		_, isOperator := scanner.wordOperators[word]
		return !continues && !isOperator
	case sqllex.KindQuotedIdentifier, sqllex.KindString, sqllex.KindParameter:
		return true
	default:
		return token.Raw == "("
	}
}

// applyCreateFunction adds or replaces a function signature in the catalogue, resolving
// the function body for SQL-language functions. A CREATE FUNCTION IF NOT EXISTS of a
// signature already present leaves it in place.
//
// Functions are filed under the key the catalogue's identifier rules give their name, as
// relations are, so PostgreSQL's quoted "Foo"() and unquoted foo() are two functions
// under two keys, and an overload is replaced only by one whose name those rules match.
// Argument and return types naming a catalogue enum, composite type or domain are
// resolved to it.
//
// Takes mutation (*ast.CatalogueMutation) which holds the function signature and body.
//
// Returns error when the mutation is missing a function signature.
func (b *catalogueBuilder) applyCreateFunction(ctx context.Context, mutation *ast.CatalogueMutation) error {
	schema := b.resolveSchema(mutation.SchemaName)
	if mutation.Function == nil || mutation.Function.Function == nil {
		return refused(errMalformedMutation, "CREATE FUNCTION mutation missing function signature")
	}
	function := mutation.Function.Function
	function.Origin = mutation.Origin

	if function.IsStrict {
		function.NullableBehaviour = sqlmodel.FunctionNullableReturnsNullOnNull
	}

	b.attachReturnsTableColumns(schema, mutation)
	b.resolveFunctionTypes(function)
	b.resolveFunctionBody(ctx, mutation.Function)

	if function.MinArguments == 0 {
		function.MinArguments = leadingRequiredArguments(function.Arguments)
	}

	functionName := b.key(function.Name)
	existing := schema.Functions[functionName]

	for index, overload := range existing {
		if b.sameName(overload.Name, function.Name) && resolve.ArgumentTypesMatch(overload.Arguments, function.Arguments) {
			if !mutation.IfNotExists {
				schema.Functions[functionName][index] = function
			}
			return nil
		}
	}

	schema.Functions[functionName] = append(schema.Functions[functionName], function)
	return nil
}

// resolveFunctionTypes resolves a function's argument and return types against the
// catalogue's enums, composite types and domains, as a column's type is resolved, so a
// function taking or returning an enum carries its values.
//
// Takes function (*sqlmodel.Function) which is the function, changed in place.
func (b *catalogueBuilder) resolveFunctionTypes(function *sqlmodel.Function) {
	for index := range function.Arguments {
		function.Arguments[index].Type = b.resolveNestedTypeName(function.Arguments[index].Type, 0)
	}
	function.ReturnType = b.resolveNestedTypeName(function.ReturnType, 0)
}

// attachReturnsTableColumns translates inline RETURNS TABLE (col type, ...) column lists,
// and the columns several output parameters give a function returning one row or a set of
// rows, into a synthetic composite type stored in the same schema, and points the
// function signature's ReturnType at that composite.
//
// Downstream catalogue consumers (TableValuedFunctionColumnsFromCatalogue,
// resolveCompositeColumns) can then resolve the function's output columns via the
// existing composite-type lookup path. This avoids introducing a new catalogue entity for
// set-returning UDF columns while still keeping the data persistent and discoverable.
//
// Takes schema (*sqlmodel.Schema) which holds the synthetic composite type.
// Takes mutation (*ast.CatalogueMutation) which carries the signature and column.
func (b *catalogueBuilder) attachReturnsTableColumns(
	schema *sqlmodel.Schema,
	mutation *ast.CatalogueMutation,
) {
	signature := mutation.Function.Function
	if len(mutation.Columns) == 0 {
		return
	}
	if signature.ReturnType.EngineName != "" {
		return
	}

	syntheticName := syntheticTableReturnTypeName(signature)
	if _, exists := schema.CompositeTypes[b.key(syntheticName)]; !exists {
		fields := make([]sqlmodel.Column, len(mutation.Columns))
		for index := range mutation.Columns {
			fields[index] = mutation.Columns[index]
			fields[index].Origin = mutation.Origin
			b.resolveCustomColumnType(&fields[index])
		}
		schema.CompositeTypes[b.key(syntheticName)] = &sqlmodel.CompositeType{
			Name:   syntheticName,
			Schema: schema.Name,
			Fields: fields,
			Origin: mutation.Origin,
		}
	}

	signature.ReturnType = sqlmodel.SQLType{
		Category:     sqlmodel.TypeCategoryComposite,
		EngineName:   syntheticName,
		Schema:       schema.Name,
		Precision:    nil,
		Scale:        nil,
		Length:       nil,
		ElementType:  nil,
		EnumValues:   nil,
		StructFields: nil,
		KeyType:      nil,
		UnionMembers: nil,
		Nullable:     false,
		Width:        0,
		Temporal:     sqlmodel.TemporalKindNone,
		Network:      sqlmodel.NetworkKindNone,
		Unsigned:     false,
		TimeZone:     false,
		Monetary:     false,
		BinaryForm:   false,
	}
}

// resolveFunctionBody analyses the function body to determine data access level and
// return type. SQL-language functions are fully parsed and analysed; other languages are
// scanned for DML keywords.
//
// When the dialect has parsed the body into the definition's Body, the shared
// engine-agnostic analyser handles it instead of the BodySQL branches. If inference
// fails, the function still registers with its existing ReturnType so downstream
// resolution can find it by name.
//
// Takes definition (*ast.FunctionDefinition) which holds the function body and the
// signature to populate.
func (b *catalogueBuilder) resolveFunctionBody(ctx context.Context, definition *ast.FunctionDefinition) {
	if definition.Body != nil {
		b.analyseBodyExpressionFunction(ctx, definition)
		return
	}

	signature := definition.Function

	if signature.BodySQL == "" {
		return
	}

	if strings.EqualFold(signature.Language, "sql") && b.analyseSQLFunctionBody(ctx, signature) {
		return
	}

	b.scanFunctionBody(signature)
}

// scanFunctionBody records the functions a body calls and, when the function declares no
// data access, classifies it by whether the body changes data.
//
// It serves the bodies the dialect cannot analyse as a query: procedural bodies, and SQL
// bodies such as a single RETURN expression that are not one complete query.
//
// Takes signature (*sqlmodel.Function) which holds the body and receives its data access.
func (b *catalogueBuilder) scanFunctionBody(signature *sqlmodel.Function) {
	scanner := newBodyScanner(b.engine)
	if _, recorded := b.calledFunctions[signature]; !recorded {
		b.calledFunctions[signature] = scanner.calledFunctions(signature.BodySQL)
	}
	if signature.DataAccess == sqlmodel.DataAccessUnknown {
		signature.DataAccess = scanner.dataAccess(signature.BodySQL)
	}
}

// analyseBodyExpressionFunction runs the shared function-body analyser against a
// definition whose Body was populated by the dialect. The local type resolver mirrors the
// construction in analyseSQLFunctionBody so both paths share the same catalogue, function
// resolver, and engine type-system port.
//
// A non-nil error from analyseFunctionBody is non-fatal: the function still registers in
// the catalogue with whatever ReturnType was previously assigned so downstream resolvers
// can look it up by name even when body inference degrades. The failure is logged at the
// internal level, and a panic is reported as a Q070 error on the statement.
//
// Takes definition (*ast.FunctionDefinition) which holds the parsed Body and
// BodyParameters and whose signature receives an inferred ReturnType when the prior
// ReturnType.Category was Unknown.
func (b *catalogueBuilder) analyseBodyExpressionFunction(ctx context.Context, definition *ast.FunctionDefinition) {
	analyseErr := b.guardBody(ctx, definition.Function, "analysing", func() error {
		functionResolver := resolve.NewFunctionResolver(b.engine.BuiltinFunctions(), b.catalogue, b.engine)
		resolver := resolve.NewTypeResolver(b.catalogue, functionResolver, b.engine)
		return analyseFunctionBody(ctx, definition, resolver)
	})
	if analyseErr != nil && !errors.Is(analyseErr, fault.ErrPanic) {
		logging.LoggerFrom(ctx).Log(ctx, logging.LevelInternal, "function body inference degraded; registering with prior return type",
			slog.String("function", definition.Name),
			logging.ErrAttr(analyseErr),
		)
	}
}

// analyseSQLFunctionBody analyses a SQL function's calls, data access and return type.
//
// Every statement of the body is analysed: the function changes data when any of them
// does, the calls of all of them are recorded, and the last statement alone gives the
// return type. If parsing or analysing any statement fails, the caller falls back to
// scanning the body so its called functions can still be recorded.
//
// Takes signature (*sqlmodel.Function) which holds the SQL body and receives the analysis
// results.
//
// Returns bool which is true when the body was analysed as queries.
func (b *catalogueBuilder) analyseSQLFunctionBody(ctx context.Context, signature *sqlmodel.Function) bool {
	analyses := b.analyseBodyStatements(ctx, signature)
	if len(analyses) == 0 {
		return false
	}
	for _, analysis := range analyses {
		if !analysis.ReadOnly && !analysedAsStatement(analysis) {
			return false
		}
	}

	b.recordBodyCalls(signature, analyses)
	if signature.DataAccess == sqlmodel.DataAccessUnknown {
		signature.DataAccess = sqlmodel.DataAccessReadOnly
		if slices.ContainsFunc(analyses, func(analysis *ast.RawQueryAnalysis) bool { return !analysis.ReadOnly }) {
			signature.DataAccess = sqlmodel.DataAccessModifiesData
		}
	}
	if signature.ReturnType.Category == sqlmodel.TypeCategoryUnknown && !signature.ReturnsSet && !b.returnsVoid(signature) {
		b.inferBodyReturnType(ctx, signature, analyses[len(analyses)-1])
	}
	return true
}

// returnsVoid reports whether a function declares that it returns nothing, by the
// dialect's void type, as a procedure without output parameters does, so its body's last
// statement does not type its result.
//
// Takes signature (*sqlmodel.Function) which is the function.
//
// Returns bool which is true when the return type is the dialect's void type.
func (b *catalogueBuilder) returnsVoid(signature *sqlmodel.Function) bool {
	void := b.engine.CoreTypes().Void
	return void.EngineName != "" && signature.ReturnType.Category == void.Category &&
		strings.EqualFold(signature.ReturnType.EngineName, void.EngineName)
}

// analyseBodyStatements parses a SQL function body and analyses each of its statements
// against the catalogue as it stands.
//
// It returns nil for a parse or analysis failure, or a body exceeding maxBodyStatements.
// Failures are logged at the internal level. A panic produces Q070, and an analysis with
// a nil node produces Q030, both reported on the statement.
//
// Takes signature (*sqlmodel.Function) which holds the SQL body.
//
// Returns []*ast.RawQueryAnalysis which holds one analysis per statement, in order, or
// nil when the body does not parse or a statement does not analyse.
func (b *catalogueBuilder) analyseBodyStatements(ctx context.Context, signature *sqlmodel.Function) []*ast.RawQueryAnalysis {
	var statements []ast.ParsedStatement
	parseError := b.guardBody(ctx, signature, "parsing", func() error {
		var err error
		statements, err = b.engine.ParseStatements(ctx, signature.BodySQL)
		return err
	})
	if parseError != nil {
		b.logBodyFallback(ctx, "function body parse degraded; scanning the body instead", signature, parseError)
		return nil
	}
	if len(statements) == 0 || len(statements) > maxBodyStatements {
		return nil
	}

	analyses := make([]*ast.RawQueryAnalysis, 0, len(statements))
	for _, statement := range statements {
		var analysis *ast.RawQueryAnalysis
		analysisError := b.guardBody(ctx, signature, "analysing", func() error {
			var err error
			analysis, err = b.engine.AnalyseQuery(ctx, b.catalogue, statement)
			return err
		})
		if analysisError != nil {
			b.logBodyFallback(ctx, "function body analysis degraded; scanning the body instead", signature, analysisError)
			return nil
		}
		if analysis == nil {
			return nil
		}
		if problem := resolve.ValidateAnalysis(analysis); problem != nil {
			b.statementDiagnostics = append(b.statementDiagnostics, diagbuild.Block(b.statement.filename, max(b.statement.line, 1),
				problem.Code, problem.Severity, fmt.Sprintf("the body of function %s: %s", signature.Name, problem.Message)))
			return nil
		}
		analyses = append(analyses, analysis)
	}
	return analyses
}

// guardBody calls work, which reads a function body through the dialect, under a panic
// guard. A recovered panic is reported as a Q070 error on the statement being applied.
//
// Takes signature (*sqlmodel.Function) which is the function whose body is read.
// Takes doing (string) which says what work does, such as "parsing".
// Takes work (func() error) which reads the body.
//
// Returns error which is work's error, or the recovered panic matching fault.ErrPanic.
func (b *catalogueBuilder) guardBody(ctx context.Context, signature *sqlmodel.Function, doing string, work func() error) error {
	err := panicguard.Run(ctx, "catalogue: "+doing+" a function body", work)
	if errors.Is(err, fault.ErrPanic) {
		b.statementDiagnostics = append(b.statementDiagnostics, diagbuild.Block(b.statement.filename, max(b.statement.line, 1),
			diag.CodeRecoveredPanic, diag.SeverityError,
			fmt.Sprintf("%s the body of function %s: %v", doing, signature.Name, err)))
	}
	return err
}

// logBodyFallback logs, at the internal level, that a function body is classified by a
// scan of its text because the dialect could not parse or analyse it. A recovered panic
// is not logged again, since its stack was logged where it was recovered.
//
// Takes message (string) which says what degraded.
// Takes signature (*sqlmodel.Function) which is the function.
// Takes err (error) which is why.
func (*catalogueBuilder) logBodyFallback(ctx context.Context, message string, signature *sqlmodel.Function, err error) {
	if errors.Is(err, fault.ErrPanic) {
		return
	}
	logging.LoggerFrom(ctx).Log(ctx, logging.LevelInternal, message,
		slog.String("function", signature.Name),
		logging.ErrAttr(err),
	)
}

// recordBodyCalls records the functions an analysed body calls, in any of its statements,
// and the table-valued functions it reads as FROM items, and warns when the body nests
// past the depth the collection follows.
//
// Takes signature (*sqlmodel.Function) which is the function whose body was analysed.
// Takes analyses ([]*ast.RawQueryAnalysis) which hold the analyses of its statements.
func (b *catalogueBuilder) recordBodyCalls(signature *sqlmodel.Function, analyses []*ast.RawQueryAnalysis) {
	called := make(map[string]struct{})
	tables := make(map[string]struct{})
	limited := false
	for _, analysis := range analyses {
		collected := collectFunctionCallsWithinLimit(analysis)
		for _, name := range collected.functions {
			called[name] = struct{}{}
		}
		for _, name := range collected.tables {
			tables[name] = struct{}{}
		}
		limited = limited || collected.limited
	}
	b.calledFunctions[signature] = sortedNames(called)
	b.calledTableFunctions[signature] = sortedNames(tables)
	if !limited {
		return
	}
	b.statementDiagnostics = append(b.statementDiagnostics, diagbuild.Block(b.statement.filename, max(b.statement.line, 1),
		diag.CodeDepthLimit, diag.SeverityWarning,
		fmt.Sprintf("the body of function %s nests more deeply than the analysis follows, so a function it calls past "+
			"the limit cannot decide whether it changes data", signature.Name)))
}

// inferBodyReturnType gives a function the type of its body's single output column, when
// the body returns exactly one.
//
// Takes signature (*sqlmodel.Function) which receives the return type.
// Takes rawAnalysis (*ast.RawQueryAnalysis) which is the body's analysis.
func (b *catalogueBuilder) inferBodyReturnType(
	ctx context.Context,
	signature *sqlmodel.Function,
	rawAnalysis *ast.RawQueryAnalysis,
) {
	builder := resolve.NewCatalogueScopeBuilder(b.engine, b.catalogue)
	scope := builder.TypeResolver.NewRootScope(resolve.ScopeKindQuery)

	_ = builder.BuildQueryScope(ctx, rawAnalysis, scope)

	outputColumns, _, _ := builder.TypeResolver.ResolveOutputColumns(
		ctx, rawAnalysis.OutputColumns, scope,
	)

	if len(outputColumns) == 1 {
		signature.ReturnType = outputColumns[0].SQLType
	}
}

// applyDropFunction removes a function or a specific overload from the catalogue: the
// overloads whose name the dialect's identifier rules match, and, when the statement
// lists argument types, whose argument types match them.
//
// The mutation must carry a FunctionSignature naming the function; a mutation without one
// is refused as malformed.
//
// The listed argument types are resolved against the catalogue's enums, composite types
// and domains first, as a created function's are.
//
// Takes mutation (*ast.CatalogueMutation) which identifies the function to drop.
//
// Returns error when the mutation is missing a function signature, or no function of the
// name and argument types exists and the statement did not say IF EXISTS.
func (b *catalogueBuilder) applyDropFunction(mutation *ast.CatalogueMutation) error {
	if mutation.Function == nil || mutation.Function.Function == nil {
		return refused(errMalformedMutation, "DROP FUNCTION mutation missing function signature")
	}
	for index := range mutation.Function.Arguments {
		mutation.Function.Arguments[index].Type = b.resolveNestedTypeName(mutation.Function.Arguments[index].Type, 0)
	}
	functionName := b.key(mutation.Function.Name)
	dropped := func(overload *sqlmodel.Function) bool {
		if overload == nil || !b.sameName(overload.Name, mutation.Function.Name) {
			return false
		}
		return len(mutation.Function.Arguments) == 0 || resolve.ArgumentTypesMatch(overload.Arguments, mutation.Function.Arguments)
	}
	schema := b.schemaHolding(mutation.SchemaName, func(candidate *sqlmodel.Schema) bool {
		return slices.ContainsFunc(candidate.Functions[functionName], dropped)
	})
	if schema == nil {
		return missingDropTarget(mutation, "function %s not found in schema %s", mutation.Function.Name, b.searchedSchemas(mutation.SchemaName))
	}
	remaining := slices.DeleteFunc(slices.Clone(schema.Functions[functionName]), dropped)
	if len(remaining) == 0 {
		delete(schema.Functions, functionName)
	} else {
		schema.Functions[functionName] = remaining
	}
	return nil
}

// applyCreateExtension registers an extension and loads any functions it provides via the
// engine's extension loader. Each function is copied, so the catalogue never shares a
// function with the dialect's registry or with another run, and filed under the key the
// catalogue's identifier rules give its name.
//
// Takes mutation (*ast.CatalogueMutation) which holds the extension name.
//
// Returns error (always nil).
func (b *catalogueBuilder) applyCreateExtension(mutation *ast.CatalogueMutation) error {
	if _, already := b.catalogue.Extensions[mutation.TableName]; already {
		return nil
	}
	b.catalogue.Extensions[mutation.TableName] = struct{}{}

	if loader, ok := b.engine.(dialect.ExtensionLoaderPort); ok {
		if functions := loader.LoadExtensionFunctions(mutation.TableName); len(functions) > 0 {
			schema := b.resolveSchema(mutation.SchemaName)
			for _, function := range functions {
				if function == nil {
					continue
				}
				key := b.key(function.Name)
				schema.Functions[key] = append(schema.Functions[key], cloneFunction(function))
			}
		}
	}

	return nil
}

// analysedAsStatement reports whether the dialect recognised a body as a statement it
// reads or writes with: one with output columns, a data change, an insert target or a
// relation.
//
// A body that is a bare expression, such as the single RETURN expression of a MySQL
// function, gives an empty analysis and is classified by a scan of its text.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the dialect's analysis of the body.
//
// Returns bool which is true when the analysis describes a statement.
func analysedAsStatement(analysis *ast.RawQueryAnalysis) bool {
	return len(analysis.OutputColumns) > 0 || analysis.DataChange != 0 || analysis.InsertTable != "" ||
		len(analysis.FromTables) > 0
}

// leadingRequiredArguments returns the count of leading arguments without a default,
// which the catalogue records as a created function's minimum when its DDL declares none.
// Optional arguments must trail the required ones, so the first optional argument marks
// the minimum; a list with no optional arguments requires all of them.
//
// Takes arguments ([]sqlmodel.FunctionArgument) which are the declared arguments.
//
// Returns int which is the count of leading required arguments.
func leadingRequiredArguments(arguments []sqlmodel.FunctionArgument) int {
	for index := range arguments {
		if arguments[index].IsOptional {
			return index
		}
	}
	return len(arguments)
}

// syntheticTableReturnTypeName builds a deterministic, unique composite-type name for a
// function's inline RETURNS TABLE columns, distinguishing overloads by encoding the
// parameter type list.
//
// The argument type EngineName plus Category together disambiguate same-schema overloads
// whose only difference is category (e.g. text vs text[] when both share EngineName
// "text" after upstream coercion).
//
// Takes signature (*sqlmodel.Function) which provides the name and arguments.
//
// Returns string which is the deterministic composite-type name.
func syntheticTableReturnTypeName(signature *sqlmodel.Function) string {
	var builder strings.Builder
	builder.WriteString("__sgl_udf_")
	builder.WriteString(signature.Name)
	builder.WriteString("__")
	for index := range signature.Arguments {
		argument := &signature.Arguments[index]
		if index > 0 {
			builder.WriteByte('_')
		}
		if argument.Type.EngineName != "" {
			builder.WriteString(argument.Type.EngineName)
		} else {
			builder.WriteString("unknown")
		}
		builder.WriteString("_c")
		builder.WriteString(strconv.Itoa(int(argument.Type.Category)))
	}
	builder.WriteString("__row")
	return builder.String()
}

// atStatementStart reports whether a token starts a statement in a body: it comes first,
// after a semicolon, after a label written <<label>>, or after a word that opens a block,
// a branch or the body of a loop.
//
// Takes tokens ([]sqllex.Token) which are the body's significant tokens.
// Takes index (int) which is the token's position.
//
// Returns bool which is true at the start of a statement.
func atStatementStart(tokens []sqllex.Token, index int) bool {
	if index == 0 {
		return true
	}
	previous := tokens[index-1].Raw
	if previous == ";" || (previous == ">" && index >= 2 && tokens[index-2].Raw == ">") {
		return true
	}
	return followsWord(tokens, index, statementOpeners)
}

// followsWord reports whether a token follows one of a set of words.
//
// Takes tokens ([]sqllex.Token) which are the body's significant tokens.
// Takes index (int) which is the token's position.
// Takes words (map[string]struct{}) which holds the words, in lower case.
//
// Returns bool which is true when the token before it is one of the words.
func followsWord(tokens []sqllex.Token, index int, words map[string]struct{}) bool {
	if index == 0 || tokens[index-1].Kind != sqllex.KindWord {
		return false
	}
	_, follows := words[strings.ToLower(tokens[index-1].Raw)]
	return follows
}

// isNameToken reports whether a token can be part of a function name: a word or a quoted
// identifier.
//
// Takes token (sqllex.Token) which is the token.
//
// Returns bool which is true for a name.
func isNameToken(token sqllex.Token) bool {
	return token.Kind == sqllex.KindWord || token.Kind == sqllex.KindQuotedIdentifier
}

// wordOperatorsOf returns the operators of a dialect's filter syntax that are spelt as a
// single word, such as ILIKE, in lower case.
//
// Takes operators ([]string) which are the dialect's further comparison operators.
//
// Returns map[string]struct{} which holds the word operators.
func wordOperatorsOf(operators []string) map[string]struct{} {
	words := make(map[string]struct{}, len(operators))
	for _, operator := range operators {
		if isWord(operator) {
			words[strings.ToLower(operator)] = struct{}{}
		}
	}
	return words
}

// isWord reports whether text is a single word of letters and underscores.
//
// Takes text (string) which is the operator's spelling.
//
// Returns bool which is true for a non-empty word.
func isWord(text string) bool {
	if text == "" {
		return false
	}
	for _, character := range text {
		if !unicode.IsLetter(character) && character != '_' {
			return false
		}
	}
	return true
}

// quotedIdentifierText returns the name a quoted identifier spells: the text between its
// quotes, with each doubled quote read as one. A bracketed identifier closes at its first
// ']', so it holds no doubled quote and its text is returned as written.
//
// Takes raw (string) which is the identifier with its quotes, at least two bytes long.
//
// Returns string which is the name.
func quotedIdentifierText(raw string) string {
	inner := raw[1 : len(raw)-1]
	if raw[0] == '[' {
		return inner
	}
	quote := raw[:1]
	return strings.ReplaceAll(inner, quote+quote, quote)
}
