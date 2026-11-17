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

import "seagull.sh/seagull/internal/dialect/dialectkit"

// mySQLAliasRefusals lists the keywords MySQL 9.7 refuses as an alias, after AS or not:
// its reserved words, as information_schema.KEYWORDS marks them and preparing SELECT 1 AS
// each keyword confirms.
const mySQLAliasRefusals = "ACCESSIBLE ADD ALL ALTER ANALYZE AND AS ASC ASENSITIVE BEFORE BETWEEN BIGINT BINARY BLOB " +
	"BOTH BY CALL CASCADE CASE CHANGE CHAR CHARACTER CHECK COLLATE COLUMN CONDITION CONSTRAINT " +
	"CONTINUE CONVERT CREATE CROSS CUBE CUME_DIST CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP " +
	"CURRENT_USER CURSOR DATABASE DATABASES DAY_HOUR DAY_MICROSECOND DAY_MINUTE DAY_SECOND DEC " +
	"DECIMAL DECLARE DEFAULT DELAYED DELETE DENSE_RANK DESC DESCRIBE DETERMINISTIC DISTINCT " +
	"DISTINCTROW DIV DOUBLE DROP DUAL EACH ELSE ELSEIF EMPTY ENCLOSED ESCAPED EXCEPT EXISTS EXIT " +
	"EXPLAIN EXTERNAL FALSE FETCH FIRST_VALUE FLOAT FLOAT4 FLOAT8 FOR FORCE FOREIGN FROM FULLTEXT FUNCTION " +
	"GENERATED GET GRANT GROUP GROUPING GROUPS HAVING HIGH_PRIORITY HOUR_MICROSECOND HOUR_MINUTE " +
	"HOUR_SECOND IF IGNORE IN INDEX INFILE INNER INOUT INSENSITIVE INSERT INT INT1 INT2 INT3 INT4 " +
	"INT8 INTEGER INTERSECT INTERVAL INTO IO_AFTER_GTIDS IO_BEFORE_GTIDS IS ITERATE JOIN " +
	"JSON_TABLE KEY KEYS KILL LAG LAST_VALUE LATERAL LEAD LEADING LEAVE LEFT LIBRARY LIKE LIMIT LINEAR " +
	"LINES LOAD LOCALTIME LOCALTIMESTAMP LOCK LONG LONGBLOB LONGTEXT LOOP LOW_PRIORITY MATCH " +
	"MAXVALUE MEDIUMBLOB MEDIUMINT MEDIUMTEXT MIDDLEINT MINUTE_MICROSECOND MINUTE_SECOND MOD " +
	"MODIFIES NATURAL NOT NO_WRITE_TO_BINLOG NTH_VALUE NTILE NULL NUMERIC OF ON OPTIMIZE " +
	"OPTIMIZER_COSTS OPTION OPTIONALLY OR ORDER OUT OUTER OUTFILE OVER PARTITION PERCENT_RANK " +
	"PRECISION PRIMARY PROCEDURE PURGE QUALIFY RANGE RANK READ READS READ_WRITE REAL RECURSIVE " +
	"REFERENCES REGEXP RELEASE RENAME REPEAT REPLACE REQUIRE RESIGNAL RESTRICT RETURN REVOKE " +
	"RIGHT RLIKE ROW ROWS ROW_NUMBER SCHEMA SCHEMAS SECOND_MICROSECOND SELECT SENSITIVE SEPARATOR " +
	"SET SHOW SIGNAL SMALLINT SPATIAL SPECIFIC SQL SQLEXCEPTION SQLSTATE SQLWARNING " +
	"SQL_BIG_RESULT SQL_CALC_FOUND_ROWS SQL_SMALL_RESULT SSL STARTING STORED STRAIGHT_JOIN SYSTEM " +
	"TABLE TABLESAMPLE TERMINATED THEN TINYBLOB TINYINT TINYTEXT TO TRAILING TRIGGER TRUE UNDO " +
	"UNION UNIQUE UNLOCK UNSIGNED UPDATE USAGE USE USING UTC_DATE UTC_TIME UTC_TIMESTAMP VALUES " +
	"VARBINARY VARCHAR VARCHARACTER VARYING VIRTUAL WHEN WHERE WHILE WINDOW WITH WRITE XOR " +
	"YEAR_MONTH ZEROFILL"

// mySQLBareAliasRefusals lists the keywords MySQL 9.7 refuses only as an alias written
// straight after a select item, where they start an operator.
const mySQLBareAliasRefusals = "MEMBER SOUNDS"

// mySQLAliasKeywords lists the keywords MySQL refuses as an output column's alias.
var mySQLAliasKeywords = dialectkit.NewAliasKeywords(mySQLAliasRefusals, mySQLAliasRefusals+" "+mySQLBareAliasRefusals)

// mySQLReservedNames lists the keywords MySQL refuses, unquoted, as a table's or a
// column's name. It is built once, since every parser and child parser reads it.
var mySQLReservedNames = dialectkit.NewReservedNames(mySQLAliasRefusals, mySQLAliasRefusals)
