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

package app

import (
	"maps"
	"slices"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/preset"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// JSONCaseAsWritten writes each json tag name as the column is named.
	JSONCaseAsWritten = emit.JSONCaseAsWritten

	// JSONCaseSnake writes each json tag name in snake case, such as created_at.
	JSONCaseSnake = emit.JSONCaseSnake

	// JSONCaseCamel writes each json tag name in lower camel case, such as createdAt.
	JSONCaseCamel = emit.JSONCaseCamel

	// JSONCasePascal writes each json tag name in upper camel case, such as CreatedAt.
	JSONCasePascal = emit.JSONCasePascal
)

// Preset bundles the settings that give generated code, diagnostics and directives their
// vocabulary: the directive prefixes, the generated-file header, the helper and error
// prefixes, the type slots and the optional query-names file.
//
// A Preset is plain data, so an embedder builds its own, starting from DefaultPreset and
// changing what it needs. Validate reports whether it can be used.
type Preset struct {
	// Types names the Go types for the categories without a single natural Go type; a zero
	// slot keeps the emitter's or the built-in default.
	Types TypeSlots

	// Header is the first line of generated files and must follow the form "// Code
	// generated <tool>. DO NOT EDIT.".
	Header string

	// HelperPrefix prefixes every generated helper identifier and, capitalised after err,
	// every generated sentinel error.
	HelperPrefix string

	// ErrorPrefix prefixes the text of every generated error and, in Go casing after Err,
	// the name of every exported generated error value.
	ErrorPrefix string

	// SQLConstantSuffix ends the name of every package-level constant holding a query's SQL,
	// after the query's name in lower camel case: the default preset's SQL gives getUserSQL
	// for a query named GetUser, keeping the constant clear of an import qualifier such as
	// context. An empty suffix leaves the query's name alone.
	SQLConstantSuffix string

	// QueryNames controls the optional query-names file.
	QueryNames QueryNames

	// StructTags adds tags to generated column fields, replacing {name} with the field's
	// JSON name. Column-specific tags take precedence; JSONTags controls the json tag.
	StructTags map[string]string

	// Interface generates an interface listing the method of every query, which Queries
	// implements, so a caller can depend on the methods it uses and a test can stand in for
	// the database. It is off by default.
	Interface Interface

	// JSONTags shapes the json tag of every generated field holding a column: its case
	// style, the omitempty option, or no json tag at all. The zero value writes the column's
	// name as it is written.
	JSONTags JSONTags

	// Directives names the directive prefixes recognised in SQL comments.
	Directives Directives

	// SQLConstantLowerCase lower-cases each underscore-separated part of a query name in SQL
	// constants and related variables. It is disabled by default.
	SQLConstantLowerCase bool

	// NonNullElements defaults collection elements to non-null. Explicit column settings,
	// type overrides and known nullability take precedence.
	NonNullElements bool

	// KeepStars preserves SELECT and RETURNING stars in generated SQL. The default expands
	// them to protect existing methods from later columns added by migrations.
	KeepStars bool

	// EmptySlices makes a many method, and a runtime builder's All, return an empty slice
	// rather than nil when no row matches, so encoding/json writes [] rather than null. It
	// is off by default.
	EmptySlices bool

	// DocComments writes doc comments from the SQL: a query's description, the prose comment
	// lines of its header, on its method, and a table's or a column's comment, such as
	// COMMENT ON TABLE gives it, on its model or field. It is off by default.
	DocComments bool

	// ReuseModels uses table models for query rows with matching field names, Go types and
	// tags. It is off by default; explicit model: or row: headers take precedence.
	ReuseModels bool

	// ModernIdioms writes the generated code in the idioms of current Go: a blank line
	// between every two top-level declarations, and iter.Seq2 as the type a stream method
	// returns. It is on by default; a preset that must reproduce code generated before it
	// existed byte for byte leaves it off.
	ModernIdioms bool

	// WrapErrors wraps every error a generated method returns with a label naming the
	// method, so a timeout or a syntax error says which query met it while errors.Is and
	// errors.As still find the error beneath, such as sql.ErrNoRows. It is off by default.
	WrapErrors bool
}

// Validate reports whether the preset can be used.
//
// Every directive prefix must match [a-z][a-z0-9_]{0,31} and appear once across the
// canonical prefix, the aliases and the foreign prefixes. The header must have the
// generated-file shape, the helper and error prefixes the directive-prefix shape, an
// enabled query-names file a plain .go file name and an identifier function name, and an
// enabled interface an identifier name. JSONTags.Case must be one of the JSONCase values,
// and each struct tag one a generated tag can hold (a key without spaces, quotes, colons
// or back quotes, and a value without back quotes or control characters), with json left
// to JSONTags.
//
// Returns error wrapping ErrInvalidPrefix (itself wrapping ErrInvalidConfig) for a bad
// prefix, or ErrInvalidConfig for any other problem, when the preset cannot be used, or
// nil.
func (p *Preset) Validate() error {
	presetSettings := p.settings()
	return presetSettings.Validate()
}

// settings converts the preset to the form the generation stages consume. The result
// shares no slice or map with the preset, so a caller that changes the preset afterwards
// does not change a generator built from it.
//
// Returns preset.Settings which holds the style, directive prefixes and type slots.
func (p *Preset) settings() preset.Settings {
	return preset.Settings{
		Style: emit.Style{
			Header:               p.Header,
			HelperPrefix:         p.HelperPrefix,
			ErrorPrefix:          p.ErrorPrefix,
			JSONScanInto:         p.Types.JSONScanInto.NotNull,
			PackageNames:         nil,
			QueryNames:           p.QueryNames,
			SQLConstantSuffix:    p.SQLConstantSuffix,
			SQLConstantLowerCase: p.SQLConstantLowerCase,
			KeepStars:            p.KeepStars,
			StructTags:           maps.Clone(p.StructTags),
			Interface:            p.Interface,
			JSONTags:             p.JSONTags,
			EmptySlices:          p.EmptySlices,
			DocComments:          p.DocComments,
			ReuseModels:          p.ReuseModels,
			ModernIdioms:         p.ModernIdioms,
			WrapErrors:           p.WrapErrors,
		},
		Prefixes: namespace.Set{
			Canonical: p.Directives.Canonical,
			Aliases:   slices.Clone(p.Directives.Aliases),
			Foreign:   slices.Clone(p.Directives.Foreign),
		},
		Types:            p.Types,
		RequireCanonical: p.Directives.RequireCanonical,
		NonNullElements:  p.NonNullElements,
	}
}

// Directives names the directive prefixes recognised in SQL comments: the canonical
// prefix that diagnostics render, the aliases accepted as synonyms, the foreign prefixes
// reported but never parsed, and whether an alias draws a hint.
type Directives struct {
	// Canonical is the prefix diagnostics render, such as sgl.
	Canonical string

	// Aliases lists prefixes accepted as synonyms for Canonical.
	Aliases []string

	// Foreign lists prefixes belonging to another tool; a directive written with one is
	// reported with a Q051 warning rather than parsed.
	Foreign []string

	// RequireCanonical reports every directive written with an alias with a Q050 hint.
	RequireCanonical bool
}

// TypeSlots selects Go types for SQL values that need custom representations or driver
// support. A zero slot keeps the default type.
type TypeSlots = sqlmodel.TypeSlots

// Slot names the Go types one category of SQL values maps to, for NOT NULL and nullable
// values. The zero value keeps the default.
type Slot = sqlmodel.TypeSlot

// GoType names a Go type by its import path and its name, such as {Package:
// "github.com/google/uuid", Name: "UUID"} or {Name: "*int64"} for a built-in type, for
// the NOT NULL and nullable halves of a Slot.
type GoType = sqlmodel.GoType

// QueryNames controls the optional generated file holding a function that maps a query's
// SQL text to its name, for labelling telemetry.
type QueryNames = emit.QueryNames

// Interface controls the optional generated interface listing the method of every query,
// which Queries implements.
type Interface = emit.Interface

// JSONTags shapes the json tags of generated fields.
type JSONTags = emit.JSONTags

// JSONCase is the case style the names in json tags are written in: JSONCaseAsWritten,
// JSONCaseSnake, JSONCaseCamel or JSONCasePascal.
type JSONCase = emit.JSONCase

// DefaultPreset returns Seagull's default configuration for directives and generated Go
// code.
//
// It uses the sgl prefix, built-in type mappings and the standard generated-file header.
// Query-name lookup generation is disabled by default.
//
// Returns Preset which is a fresh copy of the default preset.
func DefaultPreset() Preset {
	defaults := preset.DefaultSettings()
	return Preset{
		Directives: Directives{
			Canonical:        defaults.Prefixes.Canonical,
			Aliases:          defaults.Prefixes.Aliases,
			Foreign:          defaults.Prefixes.Foreign,
			RequireCanonical: defaults.RequireCanonical,
		},
		Header:               defaults.Style.Header,
		HelperPrefix:         defaults.Style.HelperPrefix,
		ErrorPrefix:          defaults.Style.ErrorPrefix,
		SQLConstantSuffix:    defaults.Style.SQLConstantSuffix,
		SQLConstantLowerCase: defaults.Style.SQLConstantLowerCase,
		QueryNames:           defaults.Style.QueryNames,
		Types:                defaults.Types,
		NonNullElements:      defaults.NonNullElements,
		KeepStars:            defaults.Style.KeepStars,
		StructTags:           defaults.Style.StructTags,
		Interface:            defaults.Style.Interface,
		JSONTags:             defaults.Style.JSONTags,
		EmptySlices:          defaults.Style.EmptySlices,
		DocComments:          defaults.Style.DocComments,
		ReuseModels:          defaults.Style.ReuseModels,
		ModernIdioms:         defaults.Style.ModernIdioms,
		WrapErrors:           defaults.Style.WrapErrors,
	}
}
