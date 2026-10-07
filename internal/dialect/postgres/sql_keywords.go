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

package postgres

const (
	// keywordALL is the SQL keyword "ALL".
	keywordALL = "ALL"

	// keywordALTER is the SQL keyword "ALTER".
	keywordALTER = "ALTER"

	// keywordAND is the SQL keyword "AND".
	keywordAND = "AND"

	// keywordAS is the SQL keyword "AS".
	keywordAS = "AS"

	// keywordASC is the SQL keyword "ASC".
	keywordASC = "ASC"

	// keywordBY is the SQL keyword "BY".
	keywordBY = "BY"

	// keywordCASCADE is the SQL keyword "CASCADE".
	keywordCASCADE = "CASCADE"

	// keywordCAST is the SQL keyword "CAST".
	keywordCAST = "CAST"

	// keywordCHECK is the SQL keyword "CHECK".
	keywordCHECK = "CHECK"

	// keywordCOLUMN is the SQL keyword "COLUMN".
	keywordCOLUMN = "COLUMN"

	// keywordCONSTRAINT is the SQL keyword "CONSTRAINT".
	keywordCONSTRAINT = "CONSTRAINT"

	// keywordCREATE is the SQL keyword "CREATE".
	keywordCREATE = "CREATE"

	// keywordCURRENT is the SQL keyword "CURRENT".
	keywordCURRENT = "CURRENT"

	// keywordDEFAULT is the SQL keyword "DEFAULT".
	keywordDEFAULT = "DEFAULT"

	// keywordDESC is the SQL keyword "DESC".
	keywordDESC = "DESC"

	// keywordDROP is the SQL keyword "DROP".
	keywordDROP = "DROP"

	// keywordEXCEPT is the SQL keyword "EXCEPT".
	keywordEXCEPT = "EXCEPT"

	// keywordEXISTS is the SQL keyword "EXISTS".
	keywordEXISTS = "EXISTS"

	// keywordFETCH is the SQL keyword "FETCH".
	keywordFETCH = "FETCH"

	// keywordFIRST is the SQL keyword "FIRST".
	keywordFIRST = "FIRST"

	// keywordFOR is the SQL keyword "FOR".
	keywordFOR = "FOR"

	// keywordFOREIGN is the SQL keyword "FOREIGN".
	keywordFOREIGN = "FOREIGN"

	// keywordFROM is the SQL keyword "FROM".
	keywordFROM = "FROM"

	// keywordGROUP is the SQL keyword "GROUP".
	keywordGROUP = "GROUP"

	// keywordHAVING is the SQL keyword "HAVING".
	keywordHAVING = "HAVING"

	// keywordINTERSECT is the SQL keyword "INTERSECT".
	keywordINTERSECT = "INTERSECT"

	// keywordJOIN is the SQL keyword "JOIN".
	keywordJOIN = "JOIN"

	// keywordKEY is the SQL keyword "KEY".
	keywordKEY = "KEY"

	// keywordLAST is the SQL keyword "LAST".
	keywordLAST = "LAST"

	// keywordLATERAL is the SQL keyword "LATERAL".
	keywordLATERAL = "LATERAL"

	// keywordLIMIT is the SQL keyword "LIMIT".
	keywordLIMIT = "LIMIT"

	// keywordMATERIALIZED is the SQL keyword "MATERIALIZED".
	keywordMATERIALIZED = "MATERIALIZED"

	// keywordNOT is the SQL keyword "NOT".
	keywordNOT = "NOT"

	// keywordNULL is the SQL keyword "NULL".
	keywordNULL = "NULL"

	// keywordNULLS is the SQL keyword "NULLS".
	keywordNULLS = "NULLS"

	// keywordOFFSET is the SQL keyword "OFFSET".
	keywordOFFSET = "OFFSET"

	// keywordON is the SQL keyword "ON".
	keywordON = "ON"

	// keywordORDER is the SQL keyword "ORDER".
	keywordORDER = "ORDER"

	// keywordPRIMARY is the SQL keyword "PRIMARY".
	keywordPRIMARY = "PRIMARY"

	// keywordRESTRICT is the SQL keyword "RESTRICT".
	keywordRESTRICT = "RESTRICT"

	// keywordRETURNING is the SQL keyword "RETURNING".
	keywordRETURNING = "RETURNING"

	// keywordROW is the SQL keyword "ROW".
	keywordROW = "ROW"

	// keywordROWS is the SQL keyword "ROWS".
	keywordROWS = "ROWS"

	// keywordSCHEMA is the SQL keyword "SCHEMA".
	keywordSCHEMA = "SCHEMA"

	// keywordSELECT is the SQL keyword "SELECT".
	keywordSELECT = "SELECT"

	// keywordSET is the SQL keyword "SET".
	keywordSET = "SET"

	// keywordTABLE is the SQL keyword "TABLE".
	keywordTABLE = "TABLE"

	// keywordTIME is the SQL keyword "TIME".
	keywordTIME = "TIME"

	// keywordTO is the SQL keyword "TO".
	keywordTO = "TO"

	// keywordTYPE is the SQL keyword "TYPE".
	keywordTYPE = "TYPE"

	// keywordUNION is the SQL keyword "UNION".
	keywordUNION = "UNION"

	// keywordUNIQUE is the SQL keyword "UNIQUE".
	keywordUNIQUE = "UNIQUE"

	// keywordUSING is the SQL keyword "USING".
	keywordUSING = "USING"

	// keywordVALUES is the SQL keyword "VALUES".
	keywordVALUES = "VALUES"

	// keywordVIEW is the SQL keyword "VIEW".
	keywordVIEW = "VIEW"

	// keywordWHERE is the SQL keyword "WHERE".
	keywordWHERE = "WHERE"

	// keywordWITH is the SQL keyword "WITH".
	keywordWITH = "WITH"

	// keywordZONE is the SQL keyword "ZONE".
	keywordZONE = "ZONE"

	// timeZoneClauseLength is the number of tokens in AT TIME ZONE, WITH TIME ZONE and
	// WITHOUT TIME ZONE.
	timeZoneClauseLength = 3

	// checkOptionClauseLength is the number of tokens in WITH CHECK OPTION.
	checkOptionClauseLength = 3

	// timezoneFunctionName is the function an AT TIME ZONE conversion calls.
	timezoneFunctionName = "timezone"

	// arraySubscriptSuffix is the trailing token denoting an array type.
	arraySubscriptSuffix = "[]"

	// decimalBase is the radix used when parsing decimal integer literals.
	decimalBase = 10

	// maxTypeModifiers is the most modifiers a PostgreSQL built-in type takes: a length, or
	// a precision and a scale.
	maxTypeModifiers = 2

	// maxDollarParameterNumber is the highest positional parameter number a `$n` placeholder
	// may carry.
	//
	// It is the most parameters one statement binds, so a larger number can never be
	// satisfied; rejecting it at the lexer keeps the parsed value within int range and
	// surfaces a clear error rather than silently clamping an overflowed value.
	maxDollarParameterNumber = maxParameters
)
