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

package mysql

import (
	"encoding/hex"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxEnumValues is the most values an ENUM holds, as MySQL and MariaDB allow.
	maxEnumValues = 65_535

	// maxSetMembers is the most members a SET holds, as MySQL and MariaDB allow.
	maxSetMembers = 64

	// bitsPerByte is how many digits of a bit literal make one byte of its value.
	bitsPerByte = 8
)

var (
	// columnAttributeKeywords lists the keywords that open a column attribute after the data
	// type, so a DEFAULT expression ends where the next attribute begins.
	columnAttributeKeywords = map[string]struct{}{
		keywordPRIMARY: {}, keywordKEY: {}, keywordNOT: {}, keywordNULL: {}, keywordUNIQUE: {},
		keywordCHECK: {}, keywordDEFAULT: {}, keywordCOLLATE: {},
		keywordCHARSET: {}, "REFERENCES": {}, "GENERATED": {}, keywordAS: {}, keywordCONSTRAINT: {},
		keywordAUTO: {}, keywordCOMMENT: {}, keywordON: {}, "VISIBLE": {}, "INVISIBLE": {},
		"COLUMN_FORMAT": {}, "STORAGE": {}, "SRID": {}, "ENGINE_ATTRIBUTE": {},
		"SECONDARY_ENGINE_ATTRIBUTE": {}, keywordUNSIGNED: {}, "SIGNED": {},
		"ZEROFILL": {}, "COMPRESSED": {}, "ENFORCED": {}, keywordFIRST: {}, keywordAFTER: {},
	}

	// columnTypeSynonyms maps two-word MySQL type spellings to the type they name.
	columnTypeSynonyms = map[string]string{
		"double precision":   "double precision",
		"character varying":  "varchar",
		"char varying":       "varchar",
		"national char":      "char",
		"national character": "char",
		"national varchar":   "varchar",
		"long varchar":       "mediumtext",
		"long varbinary":     "mediumblob",
	}

	// characterSetShorthands lists the words that follow a character type to choose its
	// character set or binary collation, such as CHAR(10) BINARY or VARCHAR(5) ASCII.
	characterSetShorthands = map[string]struct{}{
		"BINARY": {}, "ASCII": {}, "UNICODE": {}, "BYTE": {},
	}

	// binaryCharacterSetTypes maps each character type to the binary type MySQL makes of it
	// when it is declared CHARACTER SET binary: VARCHAR(n) becomes VARBINARY(n) and TEXT a
	// BLOB.
	binaryCharacterSetTypes = map[string]string{
		"char": "binary", "varchar": "varbinary", "tinytext": "tinyblob", "text": "blob",
		"mediumtext": "mediumblob", "longtext": "longblob",
	}
)

// typeAttributes is what the attributes after a column's type name say of the type.
type typeAttributes struct {
	// unsigned reports a type declared UNSIGNED or ZEROFILL, which implies UNSIGNED.
	unsigned bool

	// binaryCharacterSet reports a character type declared CHARACTER SET binary, which MySQL
	// makes the binary type of the same shape.
	binaryCharacterSet bool
}

// parseMySQLColumnDefinition parses one column definition inside CREATE TABLE or ALTER
// TABLE: the name, the data type with its numeric and character set attributes, and the
// column attributes that follow.
//
// Takes engine (*Dialect) which supplies type normalisation for the column's SQL type.
//
// Returns sqlmodel.Column which describes the parsed column.
// Returns bool which is true when the column carries an inline PRIMARY KEY.
// Returns error when the column name, type, or an attribute fails to parse.
func (p *parser) parseMySQLColumnDefinition(engine *Dialect) (sqlmodel.Column, bool, error) {
	name, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return sqlmodel.Column{}, false, fmt.Errorf("parsing column name: %w", nameError)
	}

	serial := p.IsKeyword("SERIAL") && !p.Peek().IsKeyword(tokenIdentifier, keywordDEFAULT)
	sqlType, err := p.parseColumnType(engine)
	if err != nil {
		return sqlmodel.Column{}, false, err
	}

	column := sqlmodel.NewColumn(name, sqlType, true)
	if serial {
		column.Nullable = false
		column.HasDefault = true
	}

	isPrimaryKey, err := p.parseMySQLColumnConstraints(&column)
	if err != nil {
		return sqlmodel.Column{}, false, err
	}

	return column, isPrimaryKey, nil
}

// parseColumnType parses the data type of a column definition: the type name, including
// two-word spellings such as DOUBLE PRECISION or NATIONAL VARCHAR, its modifiers, the
// UNSIGNED, SIGNED and ZEROFILL attributes, and the CHARACTER SET, CHARSET and COLLATE
// clauses mysqldump writes after a character type.
//
// Takes engine (*Dialect) which normalises the parsed type name and modifiers.
//
// Returns sqlmodel.SQLType which is the normalised column type.
// Returns error when the type's modifiers are not a list MySQL writes, or a character set
// or collation names nothing.
func (p *parser) parseColumnType(engine *Dialect) (sqlmodel.SQLType, error) {
	if p.Current().Kind != tokenIdentifier || p.IsKeyword(keywordPRIMARY, keywordNOT, keywordNULL, keywordUNIQUE,
		keywordCHECK, keywordDEFAULT, keywordCOLLATE, "REFERENCES", "GENERATED", keywordCONSTRAINT, keywordAUTO) {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), nil
	}

	lower := strings.ToLower(p.Advance().Value)
	if lower == "enum" || lower == "set" {
		return p.parseEnumOrSetType(engine, lower)
	}
	lower = p.extendTypeName(lower)

	modifiers, err := p.parseColumnTypeModifiers(lower)
	if err != nil {
		return sqlmodel.SQLType{}, err
	}

	attributes, err := p.parseTypeAttributes()
	if err != nil {
		return sqlmodel.SQLType{}, err
	}
	unsigned := attributes.unsigned
	if lower == "serial" {
		lower, unsigned = "bigint", true
	}
	if unsigned {
		lower += " " + strings.ToLower(keywordUNSIGNED)
	}
	if binary, isText := binaryCharacterSetTypes[lower]; isText && attributes.binaryCharacterSet {
		lower = binary
	}

	return engine.NormaliseTypeName(lower, modifiers...), nil
}

// extendTypeName joins the second word of a two-word type spelling onto its first, such
// as DOUBLE PRECISION or CHARACTER VARYING, and returns the type the spelling names.
//
// Takes first (string) which is the lower-cased first word of the type.
//
// Returns string which is the type name to normalise.
func (p *parser) extendTypeName(first string) string {
	if p.Current().Kind == tokenIdentifier && !p.Current().Quoted {
		if name, found := columnTypeSynonyms[first+" "+strings.ToLower(p.Current().Value)]; found {
			p.Advance()
			if p.IsKeyword("VARYING") {
				p.Advance()
				return "varchar"
			}
			return name
		}
	}
	if first == "long" {
		return "mediumtext"
	}
	return first
}

// parseTypeAttributes consumes the attributes that may follow a type name and its
// modifiers, in any order: UNSIGNED, SIGNED, ZEROFILL, CHARACTER SET or CHARSET with a
// character set name, COLLATE with a collation name, and the BINARY, ASCII, UNICODE and
// BYTE shorthands.
//
// Returns typeAttributes which says whether the type was declared UNSIGNED and whether
// its character set is binary.
// Returns error when a character set or collation clause names nothing.
func (p *parser) parseTypeAttributes() (typeAttributes, error) {
	var attributes typeAttributes
	for {
		switch {
		case p.MatchKeyword(keywordUNSIGNED), p.MatchKeyword("ZEROFILL"):
			attributes.unsigned = true
		case p.MatchKeyword("SIGNED"):
		case p.isCharacterSetStart():
			attributes.binaryCharacterSet = p.namesBinaryCharacterSet()
			if err := p.parseCharacterSetOrCollation(); err != nil {
				return typeAttributes{}, err
			}
		case p.IsKeyword(keywordCOLLATE):
			if err := p.parseCharacterSetOrCollation(); err != nil {
				return typeAttributes{}, err
			}
		case p.Current().Kind == tokenIdentifier && p.isCharacterSetShorthand():
			p.Advance()
		default:
			return attributes, nil
		}
	}
}

// namesBinaryCharacterSet reports whether the CHARACTER SET or CHARSET clause at the
// current token names the binary character set.
//
// Returns bool which is true for CHARACTER SET binary and CHARSET binary.
func (p *parser) namesBinaryCharacterSet() bool {
	name := p.Position + 1
	if p.IsKeyword(keywordCHARACTER) {
		name++
	}
	if name < len(p.Tokens) && p.Tokens[name].Kind == tokenOperator && p.Tokens[name].Value == operatorEquals {
		name++
	}
	return name < len(p.Tokens) && strings.EqualFold(p.Tokens[name].Value, "binary")
}

// isCharacterSetShorthand reports whether the current token is one of the words that
// choose a character type's character set or binary collation.
//
// Returns bool which is true for BINARY, ASCII, UNICODE or BYTE.
func (p *parser) isCharacterSetShorthand() bool {
	_, found := characterSetShorthands[p.Current().Keyword(tokenIdentifier)]
	return found
}

// isCharacterSetStart reports whether the current token opens a CHARACTER SET or CHARSET
// clause.
//
// Returns bool which is true at CHARACTER SET or CHARSET.
func (p *parser) isCharacterSetStart() bool {
	return p.IsKeyword(keywordCHARSET) ||
		(p.IsKeyword(keywordCHARACTER) && p.Peek().IsKeyword(tokenIdentifier, keywordSET))
}

// parseCharacterSetOrCollation consumes a CHARACTER SET, CHARSET or COLLATE clause and
// the name that follows it, with an optional '=' as table options write it.
//
// Returns error when the clause names no character set or collation.
func (p *parser) parseCharacterSetOrCollation() error {
	p.MatchKeyword(keywordCHARACTER)
	p.Advance()
	p.matchOperator(operatorEquals)
	if p.Current().Kind != tokenIdentifier && p.Current().Kind != tokenString {
		return p.syntaxErrorAt("expected a character set or collation name")
	}
	p.Advance()
	return nil
}

// parseEnumOrSetType parses ENUM(...) and SET(...) value lists, keeping each string value
// in order. An ENUM holds at most maxEnumValues values and a SET at most maxSetMembers
// members, as MySQL allows.
//
// Takes engine (*Dialect) which normalises the base type name.
// Takes typeName (string) which is the lowercased keyword (enum or set).
//
// Returns sqlmodel.SQLType which is the normalised type with EnumValues populated.
// Returns error which is a dialectkit.ParseError at the first value past the limit.
func (p *parser) parseEnumOrSetType(engine *Dialect, typeName string) (sqlmodel.SQLType, error) {
	limit, refusal := maxEnumValues, fmt.Sprintf("an ENUM takes at most %d values", maxEnumValues)
	if typeName == "set" {
		limit, refusal = maxSetMembers, fmt.Sprintf("a SET takes at most %d members", maxSetMembers)
	}

	values, err := p.parseEnumValues(limit, refusal)
	if err != nil {
		return sqlmodel.SQLType{}, err
	}

	sqlType := engine.NormaliseTypeName(typeName)
	sqlType.EnumValues = values
	return sqlType, nil
}

// parseEnumValues consumes the parenthesised value list of an ENUM or SET when one
// follows, keeping in order the value of each member enumMemberValue reads and passing
// over anything else.
//
// Takes limit (int) which is the most values the list may hold.
// Takes refusal (string) which says what is wrong with a list past the limit.
//
// Returns []string which holds the values in order, or nil when no list follows.
// Returns error which is a dialectkit.ParseError at the first value past limit, or at a
// malformed hexadecimal or bit literal.
func (p *parser) parseEnumValues(limit int, refusal string) ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, nil
	}
	p.Advance()

	var values []string
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		value, isMember, err := p.enumMemberValue()
		if err != nil {
			return nil, err
		}
		if isMember {
			if len(values) == limit {
				return nil, p.syntaxErrorAt(refusal)
			}
			values = append(values, value)
		}
		p.Advance()
		if p.Current().Kind == tokenComma {
			p.Advance()
		}
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return values, nil
}

// enumMemberValue reads the value of the ENUM or SET member written at the current token,
// as MySQL reads it: the text of a string literal, or the bytes a hexadecimal literal
// (X'61' or 0x61) or a bit literal (B'1100001' or 0b1100001) spells, so 0x61 is the value
// a.
//
// It consumes nothing.
//
// Returns string which is the member's value.
// Returns bool which is false when the current token is no such literal.
// Returns error which is a dialectkit.ParseError when a hexadecimal or bit literal holds
// a digit its base does not have, or X'...' holds an odd number of digits.
func (p *parser) enumMemberValue() (string, bool, error) {
	current := p.Current()
	switch current.Kind {
	case tokenString:
		return current.Value, true, nil
	case tokenHexString:
		if len(current.Value)%2 != 0 {
			return "", false, p.syntaxErrorAt("a hexadecimal literal written X'...' takes an even number of digits")
		}
		return p.decodedMemberValue(hexLiteralBytes(current.Value))
	case tokenBitString:
		return p.decodedMemberValue(bitLiteralBytes(current.Value))
	case tokenNumber:
		if digits, hexadecimal := strings.CutPrefix(current.Value, "0x"); hexadecimal && digits != "" {
			return p.decodedMemberValue(hexLiteralBytes(digits))
		}
		if digits, binary := strings.CutPrefix(current.Value, "0b"); binary && digits != "" {
			return p.decodedMemberValue(bitLiteralBytes(digits))
		}
		return "", false, nil
	default:
		return "", false, nil
	}
}

// decodedMemberValue returns the value a hexadecimal or bit literal member decoded to, or
// refuses the literal at the current token when it held a digit its base does not have.
//
// Takes value (string) which is the decoded value.
// Takes decoded (bool) which is false when the literal held such a digit.
//
// Returns string which is the member's value.
// Returns bool which is true when the member has a value.
// Returns error which is a dialectkit.ParseError when the literal did not decode.
func (p *parser) decodedMemberValue(value string, decoded bool) (string, bool, error) {
	if !decoded {
		return "", false, p.syntaxErrorAt("expected the digits of a hexadecimal or bit literal")
	}
	return value, true, nil
}

// parseTypeModifiers consumes the parenthesised modifiers that may follow a type name, as
// MySQL writes them: one to maxTypeModifiers unsigned numbers separated by commas, such
// as the length of VARCHAR(255) or the precision and scale of DECIMAL(10, 2), each read
// as typeModifierMagnitude reads it.
//
// The values of an ENUM or SET are not modifiers; parseEnumOrSetType reads them.
//
// Returns []int which holds the modifiers in order, or nil when no list follows the name.
// Returns error which is a dialectkit.ParseError when a modifier is not a number, the
// list holds more than maxTypeModifiers modifiers, or it is not closed after the last
// one.
func (p *parser) parseTypeModifiers() ([]int, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, nil
	}
	opening := p.Advance()

	modifiers := make([]int, 0, maxTypeModifiers)
	for {
		if len(modifiers) == maxTypeModifiers {
			return nil, p.syntaxErrorAt(fmt.Sprintf("a type takes at most %d modifiers", maxTypeModifiers))
		}
		value, isModifier := typeModifierMagnitude(p.Current())
		if !isModifier {
			return nil, p.syntaxErrorAt("expected a number as the type's modifier")
		}
		modifiers = append(modifiers, value)
		p.Advance()
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.AtEnd() {
		err := dialectkit.NewParseError(opening, unmatchedParenthesis)
		p.recordSyntaxError(err)
		return nil, err
	}
	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxErrorAt("expected ')' after the type's modifiers")
	}
	p.Advance()
	return modifiers, nil
}

// parseColumnTypeModifiers consumes the modifiers of a column type as parseTypeModifiers
// does, and refuses FLOAT(p) with a precision past maxDoublePrecision bits, as MySQL
// does.
//
// Takes lower (string) which is the lower-cased type name the modifiers follow.
//
// Returns []int which holds the modifiers in order, or nil when no list follows the name.
// Returns error which is a dialectkit.ParseError when the list is malformed, or at the
// precision of a FLOAT MySQL refuses.
func (p *parser) parseColumnTypeModifiers(lower string) ([]int, error) {
	precision := p.Peek()
	modifiers, err := p.parseTypeModifiers()
	if err != nil {
		return nil, err
	}
	if floatPrecisionRefused(lower, modifiers) {
		refusal := dialectkit.NewParseError(precision, floatPrecisionRefusal)
		p.recordSyntaxError(refusal)
		return nil, refusal
	}
	return modifiers, nil
}

// isColumnAttributeStart reports whether the current token opens a column attribute, such
// as NOT NULL, DEFAULT or CHARACTER SET, so a DEFAULT expression ends there.
//
// Returns bool which is true at a column attribute keyword.
func (p *parser) isColumnAttributeStart() bool {
	_, found := columnAttributeKeywords[p.Current().Keyword(tokenIdentifier)]
	return found || p.isCharacterSetStart()
}

// parseMySQLColumnConstraints applies every column attribute that follows the data type.
//
// The loop stops at the first token that opens no attribute; the caller then requires a
// ',' or ')' there, so an attribute the parser does not know fails the statement instead
// of becoming the name of a phantom column.
//
// Takes column (*sqlmodel.Column) which receives the parsed constraint flags.
//
// Returns bool which is true when the column carries an inline PRIMARY KEY.
// Returns error when an attribute clause is malformed.
func (p *parser) parseMySQLColumnConstraints(column *sqlmodel.Column) (bool, error) {
	isPrimaryKey := false

	for !p.AtEnd() && p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen {
		primary, handled, err := p.parseOneMySQLColumnConstraint(column)
		if err != nil {
			return false, err
		}
		if primary {
			isPrimaryKey = true
		}
		if !handled {
			break
		}
	}

	return isPrimaryKey, nil
}

// parseOneMySQLColumnConstraint parses a single column attribute that sets the column's
// nullability, default or key.
//
// Takes column (*sqlmodel.Column) which receives the parsed constraint flags.
//
// Returns isPrimary (bool) reporting whether the clause set PRIMARY KEY.
// Returns handled (bool) reporting whether an attribute was consumed.
// Returns err (error) which is non-nil when the clause is malformed.
func (p *parser) parseOneMySQLColumnConstraint(column *sqlmodel.Column) (isPrimary bool, handled bool, err error) {
	switch {
	case p.MatchKeyword(keywordPRIMARY), p.MatchKeyword(keywordKEY):
		p.MatchKeyword(keywordKEY)
		column.Nullable = false
		column.HasDefault = true
		return true, true, nil
	case p.MatchKeyword(keywordNOT):
		if _, err := p.ExpectKeyword(keywordNULL); err != nil {
			return false, false, err
		}
		column.Nullable = false
		return false, true, nil
	case p.MatchKeyword(keywordNULL):
		column.Nullable = true
		return false, true, nil
	case p.MatchKeyword(keywordDEFAULT):
		column.HasDefault = true
		return false, true, p.skipDefaultExpression()
	case p.MatchKeyword(keywordAUTO):
		column.HasDefault = true
		return false, true, nil
	case p.IsKeyword("SERIAL") && p.Peek().IsKeyword(tokenIdentifier, keywordDEFAULT):
		p.Advance()
		p.Advance()
		if _, err := p.ExpectKeyword("VALUE"); err != nil {
			return false, false, err
		}
		column.Nullable = false
		column.HasDefault = true
		return false, true, nil
	case p.MatchKeyword(keywordUNIQUE):
		p.MatchKeyword(keywordKEY)
		return false, true, nil
	}

	handled, err = p.parseMySQLSecondaryConstraint(column)
	return false, handled, err
}

// parseMySQLSecondaryConstraint handles the column attributes that do not set
// nullability, default or key: CHECK, REFERENCES, the generated-column forms, ON UPDATE,
// COMMENT, the character set and collation clauses, CONSTRAINT names and the storage and
// visibility options.
//
// Takes column (*sqlmodel.Column) which receives any generated-column flags discovered in
// the clause.
//
// Returns bool which reports whether a secondary clause was consumed.
// Returns error when the clause is malformed.
func (p *parser) parseMySQLSecondaryConstraint(column *sqlmodel.Column) (bool, error) {
	switch {
	case p.MatchKeyword(keywordCHECK):
		return true, p.parseCheckBody()
	case p.MatchKeyword("REFERENCES"):
		return true, p.skipMySQLInlineForeignKey()
	case p.MatchKeyword("GENERATED"):
		if _, err := p.ExpectKeyword("ALWAYS"); err != nil {
			return false, err
		}
		return true, p.parseMySQLGeneratedClause(column)
	case p.IsKeyword(keywordAS):
		return true, p.parseMySQLGeneratedClause(column)
	case p.MatchKeyword(keywordON):
		return true, p.parseOnUpdateClause()
	case p.MatchKeyword(keywordCOMMENT):
		comment := p.Current().Value
		if err := p.expectStringLiteral("a column comment"); err != nil {
			return true, err
		}
		column.Comment = comment
		return true, nil
	case p.isCharacterSetStart(), p.IsKeyword(keywordCOLLATE):
		return true, p.parseCharacterSetOrCollation()
	case p.MatchKeyword(keywordCONSTRAINT):
		if !p.IsKeyword(keywordCHECK) && p.Current().Kind == tokenIdentifier {
			p.Advance()
		}
		return true, nil
	}
	return p.parseColumnStorageOption(column)
}

// parseColumnStorageOption consumes the column options that change neither the column's
// type nor its nullability: VISIBLE and INVISIBLE, COLUMN_FORMAT, STORAGE, SRID, the
// engine attributes, MariaDB's COMPRESSED, and type attributes written after the column
// attributes.
//
// An INVISIBLE column is hidden: SELECT * and an INSERT without a column list leave it
// out, while a query may still name it.
//
// Takes column (*sqlmodel.Column) which INVISIBLE hides and VISIBLE shows.
//
// Returns bool which reports whether an option was consumed.
// Returns error when an option names no value.
func (p *parser) parseColumnStorageOption(column *sqlmodel.Column) (bool, error) {
	switch {
	case p.MatchKeyword("VISIBLE"):
		column.Hidden = false
		return true, nil
	case p.MatchKeyword("INVISIBLE"):
		column.Hidden = true
		return true, nil
	case p.MatchKeyword("COLUMN_FORMAT"), p.MatchKeyword("STORAGE"), p.MatchKeyword("SRID"):
		return true, p.expectOptionValue()
	case p.MatchKeyword("ENGINE_ATTRIBUTE"), p.MatchKeyword("SECONDARY_ENGINE_ATTRIBUTE"):
		p.matchOperator(operatorEquals)
		return true, p.expectStringLiteral("an engine attribute")
	case p.MatchKeyword("COMPRESSED"):
		if p.matchOperator(operatorEquals) {
			return true, p.expectOptionValue()
		}
		return true, nil
	case p.MatchKeyword(keywordUNSIGNED), p.MatchKeyword("SIGNED"), p.MatchKeyword("ZEROFILL"):
		return true, nil
	}
	return false, nil
}

// expectOptionValue consumes the value of an option: an identifier, number or string.
//
// Returns error when no value follows.
func (p *parser) expectOptionValue() error {
	switch p.Current().Kind {
	case tokenIdentifier, tokenNumber, tokenString:
		p.Advance()
		return nil
	default:
	}
	return p.syntaxErrorAt("expected an option value")
}

// expectStringLiteral consumes a string literal.
//
// Takes description (string) which names what the literal is for the error message.
//
// Returns error when the current token is not a string literal.
func (p *parser) expectStringLiteral(description string) error {
	if p.Current().Kind != tokenString {
		return p.syntaxErrorAt("expected a string literal for " + description)
	}
	p.Advance()
	return nil
}

// parseCheckBody consumes the parenthesised condition of a CHECK constraint and an
// optional [NOT] ENFORCED.
//
// Returns error when the condition is missing or unbalanced.
func (p *parser) parseCheckBody() error {
	if p.Current().Kind != tokenLeftParen {
		return p.syntaxErrorAt("expected '(' after CHECK")
	}
	if err := p.skipParenthesised(); err != nil {
		return err
	}
	if p.IsKeyword(keywordNOT) && p.Peek().IsKeyword(tokenIdentifier, "ENFORCED") {
		p.Advance()
	}
	p.MatchKeyword("ENFORCED")
	return nil
}

// parseMySQLGeneratedClause parses the AS (expression) [VIRTUAL | STORED | PERSISTENT]
// part of a generated column, written after GENERATED ALWAYS or alone.
//
// Takes column (*sqlmodel.Column) which receives the parsed generated column metadata.
//
// Returns error when AS or the parenthesised expression is missing or unbalanced.
func (p *parser) parseMySQLGeneratedClause(column *sqlmodel.Column) error {
	if _, err := p.ExpectKeyword(keywordAS); err != nil {
		return err
	}
	if p.Current().Kind != tokenLeftParen {
		return p.syntaxErrorAt("expected '(' after AS in a generated column")
	}
	if err := p.skipParenthesised(); err != nil {
		return err
	}
	column.IsGenerated = true
	column.GeneratedKind = sqlmodel.GeneratedKindVirtual
	if p.MatchKeyword("STORED") || p.MatchKeyword("PERSISTENT") {
		column.GeneratedKind = sqlmodel.GeneratedKindStored
	} else {
		p.MatchKeyword("VIRTUAL")
	}
	return nil
}

// parseOnUpdateClause consumes the UPDATE part of an ON UPDATE clause on a column
// definition, such as ON UPDATE CURRENT_TIMESTAMP(3).
//
// Returns error when UPDATE or its value is missing, or a precision is unbalanced.
func (p *parser) parseOnUpdateClause() error {
	if _, err := p.ExpectKeyword("UPDATE"); err != nil {
		return err
	}
	if p.Current().Kind != tokenIdentifier {
		return p.syntaxErrorAt("expected a function after ON UPDATE")
	}
	p.Advance()
	return p.skipParenthesisedIfPresent()
}

// skipMySQLInlineForeignKey consumes an inline column REFERENCES clause.
//
// Returns error when the referenced name or column list is malformed.
func (p *parser) skipMySQLInlineForeignKey() error {
	if p.Current().Kind == tokenIdentifier && !p.isMySQLForeignKeyActionBoundary() {
		if _, _, err := p.parseSchemaQualifiedName(); err != nil {
			return err
		}
	}
	if err := p.skipParenthesisedIfPresent(); err != nil {
		return err
	}
	p.skipForeignKeyActions()
	return nil
}

// isMySQLForeignKeyActionBoundary reports whether the current token marks the start (or
// just-past-end) of a foreign-key action / constraint clause. Used by
// skipMySQLInlineForeignKey to recognise the boundary between the REFERENCES
// <table>(cols) prefix and the subsequent ON / MATCH / CONSTRAINT-bearing tokens.
//
// Returns bool which is true when the current keyword is a recognised action-clause
// boundary.
func (p *parser) isMySQLForeignKeyActionBoundary() bool {
	return p.IsKeyword(keywordON, "MATCH", keywordCONSTRAINT,
		keywordPRIMARY, keywordUNIQUE, keywordKEY, keywordINDEX,
		keywordCHECK, keywordFOREIGN)
}

// skipForeignKeyActions consumes the trailing MATCH clause and the ON DELETE and ON
// UPDATE referential actions of a foreign key: RESTRICT, CASCADE, SET NULL, SET DEFAULT
// or NO ACTION.
func (p *parser) skipForeignKeyActions() {
	for {
		if p.MatchKeyword("MATCH") {
			p.MatchKeyword("FULL")
			p.MatchKeyword("PARTIAL")
			p.MatchKeyword("SIMPLE")
			continue
		}
		next := p.Peek()
		if !p.IsKeyword(keywordON) || (!next.IsKeyword(tokenIdentifier, "DELETE") && !next.IsKeyword(tokenIdentifier, "UPDATE")) {
			return
		}
		p.Advance()
		p.Advance()
		switch {
		case p.MatchKeyword(keywordSET):
			p.MatchKeyword(keywordNULL)
			p.MatchKeyword(keywordDEFAULT)
		case p.MatchKeyword(keywordNO):
			p.MatchKeyword("ACTION")
		default:
			p.MatchKeyword(keywordRESTRICT)
			p.MatchKeyword(keywordCASCADE)
		}
	}
}

// skipIndexOptions consumes trailing index option clauses: USING BTREE or HASH, COMMENT,
// KEY_BLOCK_SIZE, WITH PARSER, VISIBLE, INVISIBLE, the engine attributes, and the M and
// DISTANCE options of a MariaDB vector index.
func (p *parser) skipIndexOptions() {
	for {
		switch {
		case p.MatchKeyword(keywordUSING), p.MatchKeyword(keywordCOMMENT), p.MatchKeyword("KEY_BLOCK_SIZE"),
			p.MatchKeyword("ENGINE_ATTRIBUTE"), p.MatchKeyword("SECONDARY_ENGINE_ATTRIBUTE"),
			p.MatchKeyword("M"), p.MatchKeyword("DISTANCE"):
			p.matchOperator(operatorEquals)
			if p.Current().Kind == tokenString || p.Current().Kind == tokenNumber || p.Current().Kind == tokenIdentifier {
				p.Advance()
			}
		case p.IsKeyword(keywordWITH) && p.Peek().IsKeyword(tokenIdentifier, "PARSER"):
			p.Advance()
			p.Advance()
			if p.Current().Kind == tokenIdentifier {
				p.Advance()
			}
		case p.MatchKeyword("VISIBLE"), p.MatchKeyword("INVISIBLE"):
		default:
			return
		}
	}
}

// typeModifierMagnitude reads the magnitude of a type modifier from its number token: a
// decimal, hexadecimal or binary integer, or a fraction or exponent, which is truncated.
// A magnitude past dialectkit.MaxTypeModifier is held at it.
//
// Takes number (token) which is the modifier's number, without its sign.
//
// Returns int which is the magnitude.
// Returns bool which is false when the token is not a number.
func typeModifierMagnitude(number token) (int, bool) {
	if number.Kind != tokenNumber {
		return 0, false
	}
	return lexicalRules.Numbers.TypeModifierMagnitude(number.Value)
}

// hexLiteralBytes decodes the digits of a hexadecimal literal into the bytes they spell.
// An odd number of digits is read with a leading zero, as MySQL reads 0x123.
//
// Takes digits (string) which are the literal's digits, without its prefix or quotes.
//
// Returns string which holds the bytes.
// Returns bool which is false when a digit is not hexadecimal.
func hexLiteralBytes(digits string) (string, bool) {
	if len(digits)%2 != 0 {
		digits = "0" + digits
	}
	decoded, err := hex.DecodeString(digits)
	if err != nil {
		return "", false
	}
	return string(decoded), true
}

// bitLiteralBytes decodes the digits of a bit literal into the bytes they spell, the bits
// filling whole bytes from the right, as MySQL reads b'1100001' as the byte 0x61.
//
// Takes digits (string) which are the literal's digits, without its prefix or quotes.
//
// Returns string which holds the bytes.
// Returns bool which is false when a digit is neither 0 nor 1.
func bitLiteralBytes(digits string) (string, bool) {
	padding := (bitsPerByte - len(digits)%bitsPerByte) % bitsPerByte
	decoded := make([]byte, 0, (padding+len(digits))/bitsPerByte)
	var current byte
	for index, digit := range strings.Repeat("0", padding) + digits {
		switch digit {
		case '0':
			current <<= 1
		case '1':
			current = current<<1 | 1
		default:
			return "", false
		}
		if (index+1)%bitsPerByte == 0 {
			decoded = append(decoded, current)
			current = 0
		}
	}
	return string(decoded), true
}
