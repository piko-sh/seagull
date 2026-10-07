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

package sqlite

const (
	// keywordAS is the SQL AS keyword used for aliases.
	keywordAS = "AS"

	// keywordCAST is the SQL CAST keyword, which converts a value to a type.
	keywordCAST = "CAST"

	// keywordAND is the SQL AND boolean operator.
	keywordAND = "AND"

	// keywordBETWEEN is the SQL BETWEEN range-predicate keyword.
	keywordBETWEEN = "BETWEEN"

	// keywordBY is the SQL BY clause keyword used after GROUP and ORDER.
	keywordBY = "BY"

	// keywordCHECK is the SQL CHECK constraint keyword.
	keywordCHECK = "CHECK"

	// keywordCOLLATE is the SQL COLLATE keyword.
	keywordCOLLATE = "COLLATE"

	// keywordCONFLICT is the SQL ON CONFLICT clause keyword.
	keywordCONFLICT = "CONFLICT"

	// keywordCONSTRAINT is the SQL CONSTRAINT keyword.
	keywordCONSTRAINT = "CONSTRAINT"

	// keywordCREATE is the SQL CREATE DDL keyword.
	keywordCREATE = "CREATE"

	// keywordCROSS is the SQL CROSS join keyword.
	keywordCROSS = "CROSS"

	// keywordDROP is the SQL DROP DDL keyword.
	keywordDROP = "DROP"

	// keywordEXCEPT is the SQL EXCEPT compound operator keyword.
	keywordEXCEPT = "EXCEPT"

	// keywordEXISTS is the SQL EXISTS predicate keyword.
	keywordEXISTS = "EXISTS"

	// keywordFROM is the SQL FROM clause keyword.
	keywordFROM = "FROM"

	// keywordGROUP is the SQL GROUP clause keyword.
	keywordGROUP = "GROUP"

	// keywordFULL is the SQL FULL outer join keyword.
	keywordFULL = "FULL"

	// keywordHAVING is the SQL HAVING clause keyword.
	keywordHAVING = "HAVING"

	// keywordIF is the SQL IF keyword used in IF EXISTS clauses.
	keywordIF = "IF"

	// keywordINTERSECT is the SQL INTERSECT compound operator keyword.
	keywordINTERSECT = "INTERSECT"

	// keywordINNER is the SQL INNER join keyword.
	keywordINNER = "INNER"

	// keywordJOIN is the SQL JOIN clause keyword.
	keywordJOIN = "JOIN"

	// keywordKEY is the SQL KEY keyword used in PRIMARY KEY and FOREIGN KEY.
	keywordKEY = "KEY"

	// keywordLEFT is the SQL LEFT outer join keyword.
	keywordLEFT = "LEFT"

	// keywordLIMIT is the SQL LIMIT clause keyword.
	keywordLIMIT = "LIMIT"

	// keywordNATURAL is the SQL NATURAL join keyword.
	keywordNATURAL = "NATURAL"

	// keywordNOT is the SQL NOT boolean operator.
	keywordNOT = "NOT"

	// keywordON is the SQL ON join condition keyword.
	keywordON = "ON"

	// keywordOR is the SQL OR boolean operator.
	keywordOR = "OR"

	// keywordORDER is the SQL ORDER clause keyword.
	keywordORDER = "ORDER"

	// keywordPRIMARY is the SQL PRIMARY KEY keyword.
	keywordPRIMARY = "PRIMARY"

	// keywordREFERENCES is the SQL REFERENCES foreign key keyword.
	keywordREFERENCES = "REFERENCES"

	// keywordRETURNING is the SQL RETURNING clause keyword.
	keywordRETURNING = "RETURNING"

	// keywordRIGHT is the SQL RIGHT outer join keyword.
	keywordRIGHT = "RIGHT"

	// keywordSELECT is the SQL SELECT statement keyword.
	keywordSELECT = "SELECT"

	// keywordSET is the SQL SET clause keyword used in UPDATE statements.
	keywordSET = "SET"

	// keywordTABLE is the SQL TABLE DDL keyword.
	keywordTABLE = "TABLE"

	// keywordTRIGGER is the SQL TRIGGER DDL keyword.
	keywordTRIGGER = "TRIGGER"

	// keywordUNION is the SQL UNION compound operator keyword.
	keywordUNION = "UNION"

	// keywordUNIQUE is the SQL UNIQUE constraint keyword.
	keywordUNIQUE = "UNIQUE"

	// keywordVALUES is the SQL VALUES clause keyword used in INSERT.
	keywordVALUES = "VALUES"

	// keywordWHERE is the SQL WHERE clause keyword.
	keywordWHERE = "WHERE"

	// keywordNULL is the SQL NULL keyword, a value and a column constraint.
	keywordNULL = "NULL"

	// keywordWITH is the SQL WITH common-table-expression keyword.
	keywordWITH = "WITH"

	// minTokensForClassification is the minimum token count required before statement
	// classification inspects the second keyword.
	minTokensForClassification = 3

	// doubleArrowOperatorLength is the byte length of the `->>` JSON operator.
	doubleArrowOperatorLength = 3

	// literalTypeInteger is the type name of an integer literal that fits in 32 bits.
	literalTypeInteger = "integer"

	// literalTypeBigint is the type name of an integer literal that needs 64 bits.
	literalTypeBigint = "bigint"

	// literalTypeReal is the type name of a fractional, exponent or out-of-range literal.
	literalTypeReal = "real"

	// literalTypeText is the type name of a string literal and of other text values.
	literalTypeText = "text"

	// decimalBase is the base of a decimal integer literal.
	decimalBase = 10

	// hexadecimalBase is the base of a 0x integer literal.
	hexadecimalBase = 16
)
