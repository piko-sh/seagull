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

package neutrality

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	ruleTypeName     = "engine type name"
	ruleSchema       = "default schema"
	rulePlaceholder  = "placeholder syntax"
	ruleFamily       = "engine family"
	ruleOperator     = "engine operator"
	rulePseudoColumn = "engine pseudo-column"
	familyPrefix     = "Family"
)

var corePackageRoots = []string{
	"internal/app",
	"internal/sqlmodel",
	"internal/resolve",
	"internal/analyse",
	"internal/check",
	"internal/catalogue",
	"internal/gotypes",
	"internal/gogen",
	"internal/pipeline",
	"internal/sqltemplate",
	"internal/directive",
	"internal/sqlfile",
	"internal/emit",
}

var foldedTypeNames = setOf(
	"int2", "int4", "int8", "float4", "float8",
	"timestamptz", "timetz", "bpchar", "jsonb", "bytea", "varchar2", "nvarchar2",
	"serial", "bigserial", "smallserial", "serial2", "serial4", "serial8",
	"tinyint", "mediumint", "longtext", "mediumtext", "tinytext", "longblob", "mediumblob", "tinyblob",
	"datetime64", "hugeint", "uhugeint",
	"inet", "cidr", "macaddr", "macaddr8", "tsvector", "tsquery", "regclass", "oid",
	"money", "citext", "hstore",
)

var exactTypeNames = setOf(
	"Nullable", "LowCardinality", "FixedString", "DateTime64",
	"Int128", "Int256", "UInt8", "UInt16", "UInt32", "UInt64", "UInt128", "UInt256",
)

var foldedMapKeyTypeNames = unionOf(foldedTypeNames, lowerCased(exactTypeNames))

var ambiguousGoSpellings = setOf("int8", "Int8")

var familyNames = setOf(
	"postgres", "postgresql", "cockroachdb", "timescaledb",
	"mysql", "mariadb", "sqlite", "sqlite3", "duckdb", "clickhouse",
)

var operatorSpellings = setOf(
	"||", "->", "->>", "#>", "#>>", "@>", "<@", "?|", "?&", "::",
	"ilike", "not ilike", "glob", "regexp", "rlike", "similar to",
)

var pseudoColumnNames = setOf("rowid", "_rowid_", "ctid", "xmin", "xmax")

var goNameBuilders = setOf(
	"Ident", "NewIdent", "CachedIdent", "SelectorExpr", "SelectorExprFrom", "KeyValueIdent", "AddImport",
)

var placeholderPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\$[0-9]*$`),
	regexp.MustCompile(`^\?$`),
	regexp.MustCompile(`^@p[0-9]*$`),
	regexp.MustCompile(`^\{[A-Za-z_][A-Za-z0-9_]*:[^{}]+\}$`),
}

type offence struct {
	file string
	fact string
	rule string
	line int
}

type literalPosition struct {
	isMapKey    bool
	isDecisive  bool
	isGoName    bool
	isStructTag bool
}

type fileIndex struct {
	positions map[*ast.BasicLit]literalPosition
}

func setOf(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func unionOf(sets ...map[string]struct{}) map[string]struct{} {
	union := make(map[string]struct{})
	for _, set := range sets {
		for value := range set {
			union[value] = struct{}{}
		}
	}
	return union
}

func lowerCased(set map[string]struct{}) map[string]struct{} {
	lowered := make(map[string]struct{}, len(set))
	for value := range set {
		lowered[strings.ToLower(value)] = struct{}{}
	}
	return lowered
}

func contains(set map[string]struct{}, value string) bool {
	_, found := set[value]
	return found
}

func classifyLiteral(literal string, position literalPosition) string {
	trimmed := strings.TrimSpace(literal)
	if trimmed == "" || position.isStructTag || position.isGoName {
		return ""
	}
	folded := strings.ToLower(trimmed)
	if rule := classifyTypeName(trimmed, folded, position); rule != "" {
		return rule
	}
	switch {
	case folded == "public":
		return ruleSchema
	case contains(familyNames, folded):
		return ruleFamily
	case contains(operatorSpellings, folded):
		return ruleOperator
	case contains(pseudoColumnNames, folded):
		return rulePseudoColumn
	}
	for _, pattern := range placeholderPatterns {
		if pattern.MatchString(trimmed) {
			return rulePlaceholder
		}
	}
	return ""
}

func classifyTypeName(trimmed, folded string, position literalPosition) string {
	if position.isMapKey && contains(foldedMapKeyTypeNames, folded) {
		return ruleTypeName
	}
	if contains(exactTypeNames, trimmed) {
		return ruleTypeName
	}
	if !contains(foldedTypeNames, folded) {
		return ""
	}
	if contains(ambiguousGoSpellings, trimmed) && !position.isMapKey && !position.isDecisive {
		return ""
	}
	return ruleTypeName
}

func scanCore(root string) ([]offence, error) {
	var offences []offence
	for _, packageRoot := range corePackageRoots {
		walkErr := filepath.WalkDir(filepath.Join(root, packageRoot), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			found, scanErr := scanFile(root, path)
			if scanErr != nil {
				return scanErr
			}
			offences = append(offences, found...)
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	slices.SortFunc(offences, compareOffences)
	return offences, nil
}

func compareOffences(left, right offence) int {
	return cmp.Or(strings.Compare(left.file, right.file), cmp.Compare(left.line, right.line), strings.Compare(left.fact, right.fact))
}

func scanFile(root, path string) ([]offence, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	relative = filepath.ToSlash(relative)
	index := indexFile(file)
	var offences []offence
	record := func(node ast.Node, fact, rule string) {
		offences = append(offences, offence{file: relative, fact: fact, rule: rule, line: fileSet.Position(node.Pos()).Line})
	}
	for node := range ast.Preorder(file) {
		switch typed := node.(type) {
		case *ast.BasicLit:
			if typed.Kind != token.STRING {
				continue
			}
			value, unquoteErr := strconv.Unquote(typed.Value)
			if unquoteErr != nil {
				continue
			}
			if rule := classifyLiteral(value, index.positions[typed]); rule != "" {
				record(typed, value, rule)
			}
		case *ast.SelectorExpr:
			if fact, rule := classifySelector(typed); rule != "" {
				record(typed, fact, rule)
			}
		}
	}
	return offences, nil
}

func classifySelector(selector *ast.SelectorExpr) (string, string) {
	if !strings.HasPrefix(selector.Sel.Name, familyPrefix) {
		return "", ""
	}
	return selectorText(selector.X) + "." + selector.Sel.Name, ruleFamily
}

func selectorText(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return selectorText(typed.X) + "." + typed.Sel.Name
	default:
		return "_"
	}
}

func indexFile(file *ast.File) fileIndex {
	index := fileIndex{
		positions: make(map[*ast.BasicLit]literalPosition),
	}
	mark := func(expression ast.Expr, apply func(*literalPosition)) {
		if literal, isLiteral := expression.(*ast.BasicLit); isLiteral {
			position := index.positions[literal]
			apply(&position)
			index.positions[literal] = position
		}
	}
	for node := range ast.Preorder(file) {
		indexNode(node, mark)
	}
	return index
}

func indexNode(node ast.Node, mark func(ast.Expr, func(*literalPosition))) {
	decisive := func(position *literalPosition) { position.isDecisive = true }
	switch typed := node.(type) {
	case *ast.ImportSpec:
		mark(typed.Path, func(position *literalPosition) { position.isGoName = true })
	case *ast.Field:
		if typed.Tag != nil {
			mark(typed.Tag, func(position *literalPosition) { position.isStructTag = true })
		}
	case *ast.CompositeLit:
		if _, isMap := typed.Type.(*ast.MapType); isMap {
			for _, element := range typed.Elts {
				if pair, isPair := element.(*ast.KeyValueExpr); isPair {
					mark(pair.Key, func(position *literalPosition) { position.isMapKey = true })
				}
			}
		}
	case *ast.CaseClause:
		for _, expression := range typed.List {
			mark(expression, decisive)
		}
	case *ast.BinaryExpr:
		if typed.Op == token.EQL || typed.Op == token.NEQ {
			mark(typed.X, decisive)
			mark(typed.Y, decisive)
		}
	case *ast.ValueSpec:
		for _, expression := range typed.Values {
			mark(expression, decisive)
		}
	case *ast.CallExpr:
		if contains(goNameBuilders, calledName(typed.Fun)) {
			for _, argument := range typed.Args {
				mark(argument, func(position *literalPosition) { position.isGoName = true })
			}
		}
	}
}

func calledName(function ast.Expr) string {
	switch typed := function.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	default:
		return ""
	}
}
