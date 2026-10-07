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

package generate

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/models"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// goFileSuffix ends the name of every generated Go file.
	goFileSuffix = ".go"
)

// Package generates models, queries and required shared or driver-specific helper files.
//
// The request is checked first: a nil request, one whose package name, header or prefixes
// generated code cannot carry, one holding a naming plan that does not fit it, and one
// whose names cannot be planned are refused with an error wrapping ErrInvalidRequest
// before anything is generated. Files with empty content are dropped, a blank line parts
// every two top-level declarations of each Go file when the style asks for current
// idioms, and every remaining file is prefixed with the style's header.
//
// Takes original (*emit.Request) which carries the package, schema, queries, type
// mappings, naming plan and style. Package does not change it: a request without a naming
// plan is planned on a copy.
// Takes driver (*gogen.Driver) which describes the driver API the code calls.
//
// Returns []sqlmodel.GeneratedFile which holds the generated files in emission order.
// Returns error when the request is refused, any part fails to generate, or the context
// is done, wrapping the context's cause.
func Package(ctx context.Context, original *emit.Request, driver *gogen.Driver) ([]sqlmodel.GeneratedFile, error) {
	request, err := validateRequest(original)
	if err != nil {
		return nil, err
	}
	emission := gogen.NewEmission(request, driver)
	packageName := request.Package

	files, err := models.GenerateWithReferences(ctx, packageName, request.Catalogue, request.Types, &request.Naming, &request.Style, emission.Referenced)
	if err != nil {
		return nil, fmt.Errorf("emitting models for %s: %w", packageName, err)
	}

	queryFiles, err := emitQueries(ctx, emission)
	if err != nil {
		return nil, fmt.Errorf("emitting queries for %s: %w", packageName, err)
	}
	files = append(files, queryFiles...)

	querierFiles, err := querierFiles(ctx, emission)
	if err != nil {
		return nil, err
	}
	files = append(files, querierFiles...)

	helperFiles, err := driverHelperFiles(ctx, emission)
	if err != nil {
		return nil, err
	}
	files = append(files, helperFiles...)

	if request.Style.QueryNames.Enabled {
		namesFile, namesErr := queryNamesFile(ctx, emission)
		if namesErr != nil {
			return nil, fmt.Errorf("emitting query names for %s: %w", packageName, namesErr)
		}
		files = append(files, namesFile)
	}

	return finishPackage(ctx, emission, dropEmptyFiles(files))
}

// finishPackage adds the helper files the generated code turned out to refer to, spaces
// the declarations of every Go file when the style asks for current idioms, and prefixes
// every file with the style's header.
//
// Takes emission (*gogen.Emission) which holds the request and the identifiers the files
// refer to.
// Takes files ([]sqlmodel.GeneratedFile) which hold the files generated so far, none of
// them empty.
//
// Returns []sqlmodel.GeneratedFile which holds the package's files.
// Returns error when a helper cannot be generated, a file cannot be spaced, or the
// context is done.
func finishPackage(ctx context.Context, emission *gogen.Emission, files []sqlmodel.GeneratedFile) ([]sqlmodel.GeneratedFile, error) {
	request := emission.Request
	jsonFile, err := inlineJSONFile(ctx, emission, emission.Referenced)
	if err != nil {
		return nil, fmt.Errorf("emitting inline JSON helpers for %s: %w", request.Package, err)
	}
	conversionFile, err := convertStringsFile(ctx, emission, emission.Referenced)
	if err != nil {
		return nil, fmt.Errorf("emitting string conversions for %s: %w", request.Package, err)
	}
	files = append(files, dropEmptyFiles([]sqlmodel.GeneratedFile{jsonFile, conversionFile})...)
	if request.Style.ModernIdioms {
		if err := spaceDeclarations(files); err != nil {
			return nil, fmt.Errorf("spacing the declarations of %s: %w", request.Package, err)
		}
	}
	return applyHeader(files, request.Style.Header), nil
}

// spaceDeclarations writes a blank line between every two top-level declarations of each
// Go file, in place.
//
// Takes files ([]sqlmodel.GeneratedFile) which hold the files; only those whose name ends
// in .go are changed.
//
// Returns error when a Go file cannot be spaced.
func spaceDeclarations(files []sqlmodel.GeneratedFile) error {
	for index := range files {
		if !strings.HasSuffix(files[index].Name, goFileSuffix) {
			continue
		}
		spaced, err := goast.SpaceDeclarations(files[index].Content)
		if err != nil {
			return fmt.Errorf("%s: %w", files[index].Name, err)
		}
		files[index].Content = spaced
	}
	return nil
}

// querierFiles generates the querier file and, when the naming plan names one, the file
// of the interface of the query methods.
//
// Takes emission (*gogen.Emission) which holds the request, driver and retained query
// declarations.
//
// Returns []sqlmodel.GeneratedFile which holds the querier file, then the interface's
// file, which is empty when the plan names no interface.
// Returns error when either cannot be generated or the context is done.
func querierFiles(ctx context.Context, emission *gogen.Emission) ([]sqlmodel.GeneratedFile, error) {
	request, driver := emission.Request, emission.Driver
	if err := stopped(ctx, "the querier for "+request.Package); err != nil {
		return nil, err
	}
	querierFile, err := emitkit.Querier(request.Package, request.Naming, driver.Querier)
	if err != nil {
		return nil, fmt.Errorf("emitting querier for %s: %w", request.Package, err)
	}
	interfaceFile, err := querierInterface(ctx, emission)
	if err != nil {
		return nil, fmt.Errorf("emitting the query interface for %s: %w", request.Package, err)
	}
	return []sqlmodel.GeneratedFile{querierFile, interfaceFile}, nil
}

// driverHelperFiles generates the helper files the driver adds, and records the
// identifiers they refer to.
//
// Takes emission (*gogen.Emission) which holds the request and the driver.
//
// Returns []sqlmodel.GeneratedFile which holds the driver's helper files, or none when
// the driver adds none.
// Returns error when a helper cannot be generated or the context is done.
func driverHelperFiles(ctx context.Context, emission *gogen.Emission) ([]sqlmodel.GeneratedFile, error) {
	request := emission.Request
	if emission.Driver.Helpers == nil {
		return nil, nil
	}
	if err := stopped(ctx, "driver helpers for "+request.Package); err != nil {
		return nil, err
	}
	files, err := emission.Driver.Helpers.Helpers(request)
	if err != nil {
		return nil, fmt.Errorf("emitting driver helpers for %s: %w", request.Package, err)
	}
	maps.Copy(emission.Referenced, unqualifiedIdentifiers(files))
	return files, nil
}

// stopped returns the error emitting reports once its context is done: the context's
// cause, wrapped with what was being emitted.
//
// Takes emitting (string) which names what was being emitted.
//
// Returns error which wraps the context's cause, or nil while the context is live.
func stopped(ctx context.Context, emitting string) error {
	if cause := context.Cause(ctx); cause != nil {
		return fmt.Errorf("emitting %s: %w", emitting, cause)
	}
	return nil
}

// dropEmptyFiles removes the files with empty content, which a part returns when it
// generates nothing.
//
// Takes files ([]sqlmodel.GeneratedFile) which holds the candidate files.
//
// Returns []sqlmodel.GeneratedFile which holds the files with content, in order.
func dropEmptyFiles(files []sqlmodel.GeneratedFile) []sqlmodel.GeneratedFile {
	return slices.DeleteFunc(files, func(file sqlmodel.GeneratedFile) bool {
		return len(file.Content) == 0
	})
}

// applyHeader prefixes every file with the generated-file header followed by a blank
// line. An empty header leaves the files unchanged.
//
// Takes files ([]sqlmodel.GeneratedFile) which holds the generated files.
// Takes header (string) which is the header line, without a trailing newline.
//
// Returns []sqlmodel.GeneratedFile which holds the files with the header applied.
func applyHeader(files []sqlmodel.GeneratedFile, header string) []sqlmodel.GeneratedFile {
	if header == "" {
		return files
	}
	prefix := header + "\n\n"
	for index := range files {
		var content bytes.Buffer
		content.Grow(len(prefix) + len(files[index].Content))
		_, _ = content.WriteString(prefix)
		_, _ = content.Write(files[index].Content)
		files[index].Content = content.Bytes()
	}
	return files
}
