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

package duckdb

// duckDBReservedColumns lists the keywords DuckDB refuses as a column's name in a
// definition, read by preparing CREATE TABLE with each keyword as a column.
const duckDBReservedColumns = "ALL ANALYSE ANALYZE AND ANTI ANY ARRAY AS ASC ASOF ASYMMETRIC AT AUTHORIZATION BINARY BOTH " +
	"BY CASE CAST CHECK COLLATE COLLATION COLUMN CONCURRENTLY CONSTRAINT CREATE CROSS DEFAULT " +
	"DEFERRABLE DESC DESCRIBE DISTINCT DO ELSE END EXCEPT FALSE FETCH FOR FOREIGN FREEZE FROM " +
	"FULL GLOB GROUP HAVING ILIKE IN INITIALLY INNER INTERSECT INTO IS ISNULL JOIN LAMBDA LATERAL " +
	"LEADING LEFT LIKE LIMIT NATURAL NOT NOTNULL NULL OFFSET ON ONLY OR ORDER OUTER OVERLAPS " +
	"PIVOT PIVOT_LONGER PIVOT_WIDER PLACING POSITIONAL PRIMARY QUALIFY REFERENCES RETURNING RIGHT " +
	"SELECT SEMI SHOW SIMILAR SOME SUMMARIZE SYMMETRIC TABLE TABLESAMPLE THEN TO TRAILING TRUE " +
	"UNION UNIQUE UNPACK UNPIVOT USING VARIADIC VERBOSE WHEN WHERE WINDOW WITH"

// duckDBReservedTables lists the keywords DuckDB refuses as a table's name in a
// definition. Preparing CREATE TABLE with each keyword as the table's name refuses the
// same keywords as for a column.
const duckDBReservedTables = duckDBReservedColumns
