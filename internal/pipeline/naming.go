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
	"errors"
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// errDuplicateGeneratedFile reports an emitter that generated two files of one name, so
	// writing them would lose one.
	errDuplicateGeneratedFile = errors.New("the emitter generated two files of one name")
)

// planNaming plans the names of the package a request generates and puts the plan in the
// request. While the pipeline generates named enum and struct types, it also gives the
// type mapping the plan's namer, so the request's types resolve to them.
//
// Takes request (*emit.Request) which is the request about to be emitted; its Naming is
// set.
// Takes mappings (*gotypes.MappingTable) which is the request's type mapping.
//
// Returns []emit.Problem which holds every name that cannot be planned and every query
// whose rows do not fit the struct it asks for.
func (p *Pipeline) planNaming(request *emit.Request, mappings *gotypes.MappingTable) []emit.Problem {
	style := emitkit.StyleNames(&request.Style)
	if p.namedTypes {
		style.NamedTypes = mappings.NamesType
	}
	naming, problems := emitkit.PlanPackage(request, p.reservation, style)
	request.Naming = naming
	if p.namedTypes {
		mappings.Namer = emitkit.TypeNamer(request)
	}
	return problems
}

// validateReservedNames checks the configured style against the names the emitter
// reserves: an enabled query-names file the emitter already writes, or a query-names
// function or an enabled interface's name the generated package already declares or
// imports, is a configuration error.
//
// Takes reservation (emit.Reservation) which holds the names the emitter reserves.
// Takes style (*emit.Style) which is the configured style.
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns error which wraps fault.ErrInvalidConfig for the first collision, or nil.
func validateReservedNames(reservation emit.Reservation, style *emit.Style, info *dialect.Facts) error {
	var request emit.Request
	request.Style = *style
	request.Dialect = *info
	_, problems := emitkit.PlanPackage(&request, reservation, emitkit.NameStyle{})
	if len(problems) > 0 {
		return fmt.Errorf("%s: %w", problems[0].Message, fault.ErrInvalidConfig)
	}
	return nil
}

// problemDiagnostics reports each naming or emitter problem as a diagnostic at its
// query's file and line, or against the package for a problem with no query.
//
// Takes problems ([]emit.Problem) which are the problems.
//
// Returns []diag.SourceError which holds one diagnostic per problem.
func problemDiagnostics(problems []emit.Problem) []diag.SourceError {
	diagnostics := make([]diag.SourceError, 0, len(problems))
	for _, problem := range problems {
		filename, line := "", 0
		if problem.Query != nil {
			filename, line = problem.Query.Filename, problem.Query.Line
		}
		diagnostics = append(diagnostics, diagbuild.Block(filename, line, problem.Code, problem.Severity, problem.Message))
	}
	return diagnostics
}

// checkGeneratedFileNames refuses an emission holding two files of one name, which
// writing would silently collapse into one.
//
// Takes files ([]sqlmodel.GeneratedFile) which are the generated files.
//
// Returns error which wraps errDuplicateGeneratedFile naming the first repeated file, or
// nil.
func checkGeneratedFileNames(files []sqlmodel.GeneratedFile) error {
	seen := make(map[string]struct{}, len(files))
	for index := range files {
		if _, duplicate := seen[files[index].Name]; duplicate {
			return fmt.Errorf("%s: %w", files[index].Name, errDuplicateGeneratedFile)
		}
		seen[files[index].Name] = struct{}{}
	}
	return nil
}
