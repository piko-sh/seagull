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

package directive

import (
	"fmt"
	"strconv"
	"strings"

	"piko.sh/safeconv"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/structtag"
)

const (
	// keywordName is the keyword argument key that supplies a directive's name slot. The
	// same key fills both the positional name slot and the explicit `name:` argument, so
	// concentrating it here keeps the routing consistent across the apply helpers.
	keywordName = "name"

	// keywordStructTags is the column directive keyword that declares the struct tags of a
	// column's generated field.
	keywordStructTags = "struct_tags"

	// keywordNonNullElements is the column and parameter directive keyword that declares a
	// column's or a parameter's array elements and map values non-null or nullable.
	keywordNonNullElements = "non_null_elements"

	// KeywordModel is the query directive keyword that reads the query's rows into the model
	// of a table.
	KeywordModel = "model"

	// KeywordRow is the query directive keyword that names the struct the query's rows are
	// read into, which queries naming the same row share.
	KeywordRow = "row"
)

// applyTopCall populates the DirectiveBlock from a parsed top-level `sgl.query(...)`
// call.
//
// Positional arguments at index 0 and 1 fill the Name and Command slots respectively,
// mirroring the spec's Positionals order. Any keyword argument whose key matches a
// positional name fills the same slot, and other keyword arguments become Metadata
// entries.
//
// Takes result (*DirectiveBlock) which receives the populated directive.
// Takes call (*syntax.CallArguments) which holds the parsed positionals and keyword
// arguments.
func applyTopCall(result *DirectiveBlock, call *syntax.CallArguments) {
	span := syntax.MergeSpan(call.OpenSpan, call.CloseSpan)

	applyTopCallPositionals(result, call)

	for index := range call.KeywordArguments {
		applyTopCallKeyword(result, &call.KeywordArguments[index])
	}
	if result.Name != nil && result.Name.Span.Line == 0 {
		result.Name.Span = span
	}
}

// applyTopCallPositionals fills the Name (slot 0) and Command (slot 1) fields from
// positional arguments, mirroring the spec ordering. A non-parseable command leaves the
// Command slot empty for the keyword loop to retry later if a `command:` keyword argument
// is supplied.
//
// Takes result (*DirectiveBlock) which receives the Name and Command slots.
// Takes call (*syntax.CallArguments) which holds the parsed positional arguments.
func applyTopCallPositionals(result *DirectiveBlock, call *syntax.CallArguments) {
	if len(call.Positionals) > 0 {
		positional := &call.Positionals[0]
		result.Name = &NameDirective{
			Value:     positional.Value.Raw,
			Span:      positional.Span,
			KeySpan:   positional.Span,
			ValueSpan: positional.Span,
		}
	}
	if len(call.Positionals) >= 2 {
		positional := &call.Positionals[1]
		command, parseErr := parseCommandValue(positional.Value.Raw)
		if parseErr == nil {
			result.Command = &CommandDirective{
				Value:     positional.Value.Raw,
				Command:   command,
				Span:      positional.Span,
				KeySpan:   positional.Span,
				ValueSpan: positional.Span,
			}
		}
	}
}

// applyTopCallKeyword routes one keyword argument from the top-level query call into the
// matching Directive field, falling back to a metadata entry for keys that do not
// correspond to a known slot.
//
// Takes result (*DirectiveBlock) which receives the routed field or metadata.
// Takes keywordArgument (*syntax.ParsedKeywordArgument) which is the keyword argument to
// route.
func applyTopCallKeyword(result *DirectiveBlock, keywordArgument *syntax.ParsedKeywordArgument) {
	switch keywordArgument.Key {
	case keywordName:
		if result.Name != nil {
			return
		}
		result.Name = &NameDirective{
			Value:     keywordArgument.Value.Raw,
			Span:      keywordArgument.Span,
			KeySpan:   keywordArgument.KeySpan,
			ValueSpan: keywordArgument.ValueSpan,
		}
	case "command":
		if result.Command != nil {
			return
		}
		command, parseErr := parseCommandValue(keywordArgument.Value.Raw)
		if parseErr != nil {
			return
		}
		result.Command = &CommandDirective{
			Value:     keywordArgument.Value.Raw,
			Command:   command,
			Span:      keywordArgument.Span,
			KeySpan:   keywordArgument.KeySpan,
			ValueSpan: keywordArgument.ValueSpan,
		}
	default:
		result.Metadata = append(result.Metadata, &MetadataDirective{
			Directive: keywordArgument.Key,
			Value:     keywordArgument.Value.Raw,
			Span:      keywordArgument.Span,
			KeySpan:   keywordArgument.KeySpan,
			ValueSpan: keywordArgument.ValueSpan,
		})
	}
}

// applyHeaderCall populates the DirectiveBlock from a parsed header directive call.
//
// The call dispatches to the embed, column, or sortable role. The table/name positional
// may be passed by name (`table:` / `name:`), in which case it is routed through the
// keyword argument loop into the same slot.
//
// Takes result (*DirectiveBlock) which receives the populated directive.
// Takes spec (*DirectiveSpec) which selects the header directive to apply.
// Takes call (*syntax.CallArguments) which holds the parsed positionals and keyword
// arguments.
// Takes errorBuilder (diagbuild.ErrorBuilder) which builds source-mapped diagnostics.
// Takes diagnostics (*[]diag.SourceError) which accumulates emitted diagnostics.
// Takes directiveName (string) which is the canonical directive name for diagnostics.
func applyHeaderCall(result *DirectiveBlock, spec *DirectiveSpec, call *syntax.CallArguments, errorBuilder diagbuild.ErrorBuilder, diagnostics *[]diag.SourceError, directiveName string) {
	switch spec.Name {
	case RoleEmbed:
		applyEmbedHeaderCall(result, call)
	case RoleColumn:
		applyColumnHeaderCall(result, call, errorBuilder, diagnostics, directiveName)
	case roleSortable:
		applySortableHeaderCall(result, spec, call)
	}
}

// applySortableHeaderCall builds a sortable parameter directive from a standalone
// sortable(name, [columns]) call. Sortable does not bind a placeholder, so it carries no
// anchor or number here; ResolveParameters assigns it a number after the bound parameters
// so it appears last in the generated params struct.
//
// Takes result (*DirectiveBlock) which receives the sortable directive.
// Takes spec (*DirectiveSpec) which supplies the directive name and slots.
// Takes call (*syntax.CallArguments) which holds the parsed positionals and keyword
// arguments.
func applySortableHeaderCall(result *DirectiveBlock, spec *DirectiveSpec, call *syntax.CallArguments) {
	name, nameSpan, ok := resolveDirectiveName(call)
	if !ok {
		return
	}
	directive := &ParameterDirective{
		Name:            name,
		Anchor:          "",
		DirectiveName:   spec.Name,
		Kind:            sqlmodel.ParameterDirectiveSortable,
		Span:            syntax.MergeSpan(call.OpenSpan, call.CloseSpan),
		KindSpan:        syntax.MergeSpan(call.OpenSpan, call.CloseSpan),
		NameSpan:        nameSpan,
		TypeHint:        nil,
		Nullable:        nil,
		DefaultValue:    nil,
		MaxValue:        nil,
		NonNullElements: nil,
		Columns:         nil,
		Number:          0,
		NumberSpan:      diagbuild.TextSpan{},
		IsNamed:         false,
		IsOptional:      false,
		IsSlice:         false,
	}
	if len(call.Positionals) >= 2 && len(spec.Positionals) >= 2 {
		applyParameterValue(directive, spec.Positionals[1].Name, &call.Positionals[1].Value)
	}
	for index := range call.KeywordArguments {
		keywordArgument := &call.KeywordArguments[index]
		if keywordArgument.Key == keywordName {
			continue
		}
		applyParameterValue(directive, keywordArgument.Key, &keywordArgument.Value)
	}
	result.Parameters = append(result.Parameters, directive)
}

// resolveDirectiveName returns the directive's parameter name from positional slot 0 when
// present, otherwise from a 'name:' keyword argument, so the apply phase honours both the
// positional and keyword spellings the validator accepts. ok is false when neither
// supplies a name.
//
// Takes call (*syntax.CallArguments) which holds the parsed positionals and keyword
// arguments.
//
// Returns the name, its source span, and whether a name was found.
func resolveDirectiveName(call *syntax.CallArguments) (string, diagbuild.TextSpan, bool) {
	if len(call.Positionals) > 0 {
		return call.Positionals[0].Value.Raw, call.Positionals[0].Span, true
	}
	for index := range call.KeywordArguments {
		if call.KeywordArguments[index].Key == keywordName {
			return call.KeywordArguments[index].Value.Raw, call.KeywordArguments[index].ValueSpan, true
		}
	}
	return "", diagbuild.TextSpan{}, false
}

// applyEmbedHeaderCall populates an EmbedDirective from a parsed embed(...) call.
//
// Takes result (*DirectiveBlock) which receives the embed directive.
// Takes call (*syntax.CallArguments) which holds the parsed positionals and keyword
// arguments.
func applyEmbedHeaderCall(result *DirectiveBlock, call *syntax.CallArguments) {
	embed := &EmbedDirective{
		Span:      syntax.MergeSpan(call.OpenSpan, call.CloseSpan),
		Table:     "",
		From:      "",
		As:        "",
		TableSpan: diagbuild.TextSpan{},
		FromSpan:  diagbuild.TextSpan{},
		AsSpan:    diagbuild.TextSpan{},
	}
	if len(call.Positionals) > 0 {
		embed.Table = call.Positionals[0].Value.Raw
		embed.TableSpan = call.Positionals[0].Span
	}
	for index := range call.KeywordArguments {
		keywordArgument := &call.KeywordArguments[index]
		switch keywordArgument.Key {
		case "table":
			if embed.Table == "" {
				embed.Table = keywordArgument.Value.Raw
				embed.TableSpan = keywordArgument.ValueSpan
			}
		case "from":
			embed.From = keywordArgument.Value.Raw
			embed.FromSpan = keywordArgument.ValueSpan
		case "as":
			embed.As = keywordArgument.Value.Raw
			embed.AsSpan = keywordArgument.ValueSpan
		}
	}
	result.Embeds = append(result.Embeds, embed)
	result.Metadata = append(result.Metadata, &MetadataDirective{
		Directive: "embed",
		Value:     embed.Table,
		Span:      embed.Span,
		KeySpan:   diagbuild.TextSpan{},
		ValueSpan: diagbuild.TextSpan{},
	})
}

// applyColumnHeaderCall populates a ColumnOverride from a parsed column(...) call.
//
// Reports mutually exclusive `type:` and `go_type:` keyword arguments while retaining the
// parsed values for diagnostics. A call that names no column, by position or with name:,
// records no override, since the call's validation reports the missing name.
//
// Takes result (*DirectiveBlock) which receives the column override.
// Takes call (*syntax.CallArguments) which holds the parsed positionals and keyword
// arguments.
// Takes errorBuilder (diagbuild.ErrorBuilder) which builds source-mapped diagnostics.
// Takes diagnostics (*[]diag.SourceError) which accumulates emitted diagnostics.
// Takes directiveName (string) which is the canonical directive name for diagnostics.
func applyColumnHeaderCall(result *DirectiveBlock, call *syntax.CallArguments, errorBuilder diagbuild.ErrorBuilder, diagnostics *[]diag.SourceError, directiveName string) {
	override := &ColumnOverride{}
	override.Span = syntax.MergeSpan(call.OpenSpan, call.CloseSpan)
	name, nameSpan, named := resolveDirectiveName(call)
	override.Name = name
	override.NameSpan = nameSpan

	var sawType, sawGoType bool
	var goTypeSpan diagbuild.TextSpan
	for index := range call.KeywordArguments {
		keywordArgument := &call.KeywordArguments[index]
		switch keywordArgument.Key {
		case "type":
			override.SQLType = keywordArgument.Value.Raw
			override.TypeSpan = keywordArgument.ValueSpan
			sawType = true
		case "go_type":
			override.GoType = keywordArgument.Value.Raw
			override.GoTypeSpan = keywordArgument.ValueSpan
			goTypeSpan = keywordArgument.Span
			sawGoType = true
		case "nullable":
			override.Nullable = new(strings.EqualFold(keywordArgument.Value.Raw, literalTrue))
		case keywordNonNullElements:
			override.NonNullElements = new(strings.EqualFold(keywordArgument.Value.Raw, literalTrue))
		case keywordStructTags:
			*diagnostics = append(*diagnostics, readStructTags(override, keywordArgument, errorBuilder)...)
		}
	}

	if sawType && sawGoType {
		*diagnostics = append(*diagnostics, errorBuilder.MutuallyExclusiveKeywordArgument(goTypeSpan, directiveName, "type", "go_type"))
	}
	if sawGoType {
		goType, problem := ParseGoTypeOverride(override.GoType)
		override.ParsedGoType = goType
		if problem != "" {
			*diagnostics = append(*diagnostics, unusableGoTypeDiagnostic(override, problem, errorBuilder))
		}
	}

	if !named {
		return
	}
	result.ColumnOverrides = append(result.ColumnOverrides, override)
}

// readStructTags reads a column directive's struct_tags value into its override,
// reporting a value that is not a struct tag as Go writes one, or holds a tag a generated
// field cannot carry, as a Q028 error on the value.
//
// Takes override (*ColumnOverride) which receives the tags and names the column.
// Takes keywordArgument (*syntax.ParsedKeywordArgument) which holds the value and its
// span.
// Takes errorBuilder (diagbuild.ErrorBuilder) which builds source-mapped diagnostics.
//
// Returns []diag.SourceError which holds the diagnostic, or nil when the tags are usable.
func readStructTags(override *ColumnOverride, keywordArgument *syntax.ParsedKeywordArgument, errorBuilder diagbuild.ErrorBuilder) []diag.SourceError {
	tags, err := structtag.Parse(keywordArgument.Value.Raw)
	if err != nil {
		return []diag.SourceError{errorBuilder.At(
			keywordArgument.ValueSpan,
			diag.CodeInvalidKeywordArgumentValue,
			fmt.Sprintf("struct_tags for column %q: %s", override.Name, err),
		)}
	}
	override.StructTags = tags
	return nil
}

// buildParameterDirective converts a parsed parameter-binding call into a
// ParameterDirective AST node.
//
// Positional 0 supplies the parameter name when present; a name: keyword supplies it
// otherwise. Other positional and keyword arguments fill the slots the spec declares.
//
// Takes spec (*DirectiveSpec) which supplies the directive name and slots.
// Takes call (*syntax.CallArguments) which holds the parsed positionals and keyword
// arguments.
// Takes anchorToken (syntax.Token) which anchors the directive span to the placeholder.
// Takes kindSpan (diagbuild.TextSpan) which spans the directive kind keyword.
// Takes isNamed (bool) which records whether the bound placeholder is named.
// Takes number (int) which is the parameter number assigned to the directive.
//
// Returns *ParameterDirective which is the built directive, or nil when no name was
// supplied.
func buildParameterDirective(spec *DirectiveSpec, call *syntax.CallArguments, anchorToken syntax.Token, kindSpan diagbuild.TextSpan, isNamed bool, number int) *ParameterDirective {
	name, nameSpan, ok := resolveDirectiveName(call)
	if !ok {
		return nil
	}
	directive := &ParameterDirective{
		Name:            name,
		Anchor:          "",
		DirectiveName:   spec.Name,
		Number:          number,
		IsNamed:         isNamed,
		Kind:            spec.ParamKind,
		Span:            syntax.MergeSpan(anchorToken.Span, call.CloseSpan),
		NumberSpan:      anchorToken.Span,
		KindSpan:        kindSpan,
		NameSpan:        nameSpan,
		TypeHint:        nil,
		Nullable:        nil,
		DefaultValue:    nil,
		MaxValue:        nil,
		NonNullElements: nil,
		Columns:         nil,
		IsOptional:      false,
		IsSlice:         false,
	}
	if len(call.Positionals) >= 2 && len(spec.Positionals) >= 2 {
		applyParameterValue(directive, spec.Positionals[1].Name, &call.Positionals[1].Value)
	}
	for index := range call.KeywordArguments {
		keywordArgument := &call.KeywordArguments[index]
		if keywordArgument.Key == keywordName {
			continue
		}
		applyParameterValue(directive, keywordArgument.Key, &keywordArgument.Value)
	}
	return directive
}

// applyParameterValue routes a value (from positional slot 1 or from any keyword
// argument) into the corresponding ParameterDirective field.
//
// Takes directive (*ParameterDirective) which receives the routed value.
// Takes key (string) which selects the directive field to populate.
// Takes value (*syntax.ParsedValue) which carries the raw value and any list elements.
func applyParameterValue(directive *ParameterDirective, key string, value *syntax.ParsedValue) {
	switch key {
	case "type":
		directive.TypeHint = new(value.Raw)
	case "nullable":
		directive.Nullable = new(strings.EqualFold(value.Raw, literalTrue))
	case "default":
		if parsed, parseErr := strconv.ParseInt(value.Raw, 10, 64); parseErr == nil {
			directive.DefaultValue = new(safeconv.Int64ToInt(parsed))
		}
	case "max":
		if parsed, parseErr := strconv.ParseInt(value.Raw, 10, 64); parseErr == nil {
			directive.MaxValue = new(safeconv.Int64ToInt(parsed))
		}
	case "optional":
		directive.IsOptional = strings.EqualFold(value.Raw, literalTrue)
	case "kind":
		directive.IsSlice = strings.EqualFold(value.Raw, "slice")
	case keywordNonNullElements:
		directive.NonNullElements = new(strings.EqualFold(value.Raw, literalTrue))
	case "columns":
		directive.Columns = append(directive.Columns, value.AsList...)
	}
}
