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

package clickhouse

import "strings"

// typeAliasSpace separates words in native type aliases.
const typeAliasSpace = " "

// clickHouseTypeAliases maps the SQL compatibility spellings ClickHouse accepts for its
// types, in lower case, to the native type each names.
var clickHouseTypeAliases = map[string]string{
	"tinyint": "Int8", "int1": "Int8", "byte": "Int8", "tinyint signed": "Int8", "int1 signed": "Int8",
	"smallint": "Int16", "smallint signed": "Int16",
	"int": "Int32", "integer": "Int32", "mediumint": "Int32", "mediumint signed": "Int32", "int signed": "Int32", "integer signed": "Int32",
	"bigint": "Int64", "signed": "Int64", "bigint signed": "Int64",
	"tinyint unsigned": "UInt8", "int1 unsigned": "UInt8", "smallint unsigned": "UInt16", "year": "UInt16",
	"mediumint unsigned": "UInt32", "int unsigned": "UInt32", "integer unsigned": "UInt32",
	"unsigned": "UInt64", "bigint unsigned": "UInt64", "bit": "UInt64", "set": "UInt64",
	"float": "Float32", "real": "Float32", "single": "Float32", "double": "Float64", "double precision": "Float64",
	"char": "String", "nchar": "String", "character": "String", "varchar": "String", "nvarchar": "String", "varchar2": "String",
	"text": "String", "tinytext": "String", "mediumtext": "String", "longtext": "String", "blob": "String", "clob": "String",
	"tinyblob": "String", "mediumblob": "String", "longblob": "String", "bytea": "String", "varbinary": "String",
	"character large object": "String", "character varying": "String", "char large object": "String", "char varying": "String",
	"national char": "String", "national character": "String", "national character large object": "String", "national character varying": "String",
	"national char varying": "String", "nchar varying": "String", "nchar large object": "String", "binary large object": "String", "binary varying": "String",
}

// clickHouseTypeAliasPrefixes holds every alias and every run of its leading words, so a
// reader can tell in one lookup whether another word may follow.
var clickHouseTypeAliasPrefixes = typeAliasPrefixes(clickHouseTypeAliases)

// typeAliasPrefixes lists every alias and every run of its leading words.
//
// Takes aliases (map[string]string) which maps each alias to its native type.
//
// Returns map[string]struct{} which holds the aliases and their leading runs of words.
func typeAliasPrefixes(aliases map[string]string) map[string]struct{} {
	prefixes := make(map[string]struct{}, len(aliases))
	for alias := range aliases {
		for index := range len(alias) {
			if alias[index] == typeAliasSpace[0] {
				prefixes[alias[:index]] = struct{}{}
			}
		}
		prefixes[alias] = struct{}{}
	}
	return prefixes
}

// typeAliasPrefix reports whether the words read so far are an alias or the start of one.
//
// Takes name (string) which contains the words read so far.
//
// Returns bool which permits another alias word to be consumed.
func typeAliasPrefix(name string) bool {
	_, found := clickHouseTypeAliasPrefixes[strings.ToLower(name)]
	return found
}

// readScalarAlias consumes the words of an alias without reading past the type.
//
// Takes identifier (string) which is the leading type word.
//
// Returns string which is the alias as written, or identifier when no word follows.
func (p *typeNameParser) readScalarAlias(identifier string) string {
	for typeAliasPrefix(identifier) {
		position := p.position
		p.skipWhitespace()
		word := p.parseIdentifier()
		if word == "" || !typeAliasPrefix(identifier+typeAliasSpace+word) {
			p.position = position
			break
		}
		identifier += typeAliasSpace + word
	}
	return identifier
}

// parseScalarAlias resolves a compatibility alias and the display width ClickHouse
// ignores after it.
//
// Takes identifier (string) which starts the scalar type.
//
// Returns typeParseResult which is the native type.
// Returns error when a supplied display width is malformed.
func (p *typeNameParser) parseScalarAlias(identifier string) (typeParseResult, error) {
	identifier = p.readScalarAlias(identifier)
	if _, alias := clickHouseTypeAliases[strings.ToLower(identifier)]; alias {
		p.skipWhitespace()
		if p.match('(') {
			if _, err := p.parseIntegerModifier(); err != nil {
				return typeParseResult{}, err
			}
			p.skipWhitespace()
			if !p.match(')') {
				return typeParseResult{}, p.errorAt("expected ')' after alias display width")
			}
			identifier = p.readScalarAlias(identifier)
		}
	}
	return p.parsePrimitiveByName(identifier)
}

// readColumnTypeAlias collects the words of a compatibility alias on either side of an
// optional display width.
//
// Takes builder (*strings.Builder) which receives the written type.
// Takes name (string) which holds the alias words read so far, without the width.
//
// Returns string which is the alias words read so far.
func (p *parser) readColumnTypeAlias(builder *strings.Builder, name string) string {
	for p.Current().Kind == tokenIdentifier && typeAliasPrefix(name+typeAliasSpace+p.Current().Value) {
		name += typeAliasSpace + p.Current().Value
		builder.WriteByte(' ')
		builder.WriteString(p.Current().Value)
		p.Advance()
	}
	return name
}
