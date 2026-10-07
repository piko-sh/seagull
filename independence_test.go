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

package seagull_test

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit"
)

const (
	modulePath    = "seagull.sh/seagull"
	dialectRoot   = modulePath + "/internal/dialect/"
	workspaceAuto = "GOWORK=auto"
)

var (
	sharedDialectPackages = []string{"ast", "dialectkit"}
	dialectTooling        = []string{"dialects", "dialecttest"}
	declaredBases         = map[string]string{
		"cockroachdb": "postgres",
		"timescaledb": "postgres",
		"mariadb":     "mysql",
	}
	builtInDialects = []string{"clickhouse", "cockroachdb", "duckdb", "mariadb", "mysql", "postgres", "sqlite", "timescaledb"}
	lexicalLeaf     = regexp.MustCompile("^" + regexp.QuoteMeta(dialectRoot) + "[a-z]+/lexical$")
	libraryPackages = []string{
		".", "./sdk/...", "./internal/diag/...", "./internal/dialect/...", "./internal/emit/...", "./internal/schema/...",
		"./internal/migrate/...", "./internal/observe/...", "./internal/sqllex/...", "./internal/sqlmodel/...",
	}
	allowedThirdPartyModules = map[string]string{
		"piko.sh/goastutil":            "builds the generated Go syntax trees",
		"piko.sh/safeconv":             "checked integer conversions",
		"piko.sh/safedisk":             "confined file reads and writes",
		"github.com/cespare/xxhash/v2": "goastutil's short hashes for disambiguated identifiers",
		"golang.org/x/tools":           "goastutil's syntax-tree rewriting, through x/tools/go/ast/astutil",
	}
	runtimePackages = []string{
		modulePath + "/internal/diag",
		modulePath + "/internal/diagbuild",
		modulePath + "/internal/directive/syntax",
		modulePath + "/internal/fault",
		modulePath + "/internal/logging",
		modulePath + "/internal/namespace",
		modulePath + "/internal/observe",
		modulePath + "/internal/panicguard",
		modulePath + "/internal/sqlfile",
		modulePath + "/internal/sqllex",
	}
)

func goList(t *testing.T, arguments ...string) []string {
	t.Helper()

	output, err := testkit.GoCommand(t, "", []string{workspaceAuto}, append([]string{"list"}, arguments...)...).Output()
	require.NoError(t, err, "go list %s", strings.Join(arguments, " "))
	listed := strings.Fields(string(output))
	require.NotEmpty(t, listed, "go list %s matched nothing", strings.Join(arguments, " "))
	return listed
}

func dialectOwning(importPath string) string {
	if !strings.HasPrefix(importPath, dialectRoot) {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(importPath, dialectRoot), "/")
	if slices.Contains(sharedDialectPackages, first) {
		return ""
	}
	return first
}

func listDialects(t *testing.T) []string {
	t.Helper()

	var dialects []string
	for _, importPath := range goList(t, "./internal/dialect/...") {
		owner := dialectOwning(importPath)
		if owner != "" && !slices.Contains(dialectTooling, owner) && importPath == dialectRoot+owner {
			dialects = append(dialects, owner)
		}
	}
	return dialects
}

func TestEachDialectBuildsOnlyItselfAndItsDeclaredBase(t *testing.T) {
	if testing.Short() {
		t.Skip("loading the dependency graph runs go list; skipped under -short")
	}

	dialects := listDialects(t)
	require.Subset(t, dialects, builtInDialects, "every built-in dialect is found under internal/dialect/")
	for _, name := range dialects {
		allowed := []string{name}
		if base, ok := declaredBases[name]; ok {
			allowed = append(allowed, base)
		}
		for _, dependency := range goList(t, "-deps", "./internal/dialect/"+name+"/...") {
			owner := dialectOwning(dependency)
			require.True(t, owner == "" || slices.Contains(allowed, owner),
				"dialect %s pulls in %s, which belongs to dialect %s; a dialect may build on its declared base only (%v)",
				name, dependency, owner, allowed)
		}
	}
}

func TestTheMigrationRuntimeLinksNoAnalyserOrDialectParser(t *testing.T) {
	if testing.Short() {
		t.Skip("loading the dependency graph runs go list; skipped under -short")
	}

	for _, dependency := range goList(t, "-deps", "./internal/migrate/...", "./sdk/migrate/...") {
		if !strings.HasPrefix(dependency, modulePath+"/") {
			continue
		}
		allowed := strings.HasPrefix(dependency, modulePath+"/internal/migrate") ||
			strings.HasPrefix(dependency, modulePath+"/sdk/migrate") ||
			lexicalLeaf.MatchString(dependency) ||
			slices.Contains(runtimePackages, dependency)
		require.True(t, allowed,
			"the migration run time links %s; it may link only its own packages, sqllex, the dialects' lexical leaves and the shared run-time packages %v",
			dependency, runtimePackages)
	}
}

func TestTheLibraryLinksOnlyItsDeclaredThirdPartyModules(t *testing.T) {
	if testing.Short() {
		t.Skip("loading the dependency graph runs go list; skipped under -short")
	}

	arguments := append([]string{"-deps", "-f", "{{if not .Standard}}{{with .Module}}{{.Path}}{{end}}{{end}}"}, libraryPackages...)
	linked := make(map[string]bool)
	for _, module := range goList(t, arguments...) {
		if module != modulePath {
			linked[module] = true
		}
	}

	require.Equal(t, slices.Sorted(maps.Keys(allowedThirdPartyModules)), slices.Sorted(maps.Keys(linked)),
		"the library packages an embedder imports link exactly the declared third-party modules; "+
			"a new module needs a reason in allowedThirdPartyModules and docs/reference/generator-api.md, "+
			"and a module no longer linked leaves both")
}
