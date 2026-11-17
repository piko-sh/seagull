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

package cockroachdb

// cockroachDBReservedColumns lists the keywords CockroachDB 26.4 refuses as a column's
// name in a definition, read by preparing CREATE TABLE with each keyword as a column.
const cockroachDBReservedColumns = "ALL ANALYSE ANALYZE AND ANY ARRAY AS ASC ASYMMETRIC AUTHORIZATION BOTH CASE CAST CHECK " +
	"COLLATE COLLATION COLUMN CONCURRENTLY CONSTRAINT CREATE CROSS CURRENT_CATALOG CURRENT_DATE " +
	"CURRENT_ROLE CURRENT_SCHEMA CURRENT_TIME CURRENT_TIMESTAMP CURRENT_USER DEFAULT DEFERRABLE " +
	"DESC DISTINCT DO ELSE END EXCEPT FALSE FAMILY FETCH FOR FOREIGN FROM FULL GRANT GROUP HAVING " +
	"ILIKE IN INITIALLY INNER INTERSECT INTO IS ISNULL JOIN LATERAL LEADING LEFT LIMIT LOCALTIME " +
	"LOCALTIMESTAMP NATURAL NONE NOT NOTNULL NULL OFFSET ON ONLY OR ORDER OUTER OVERLAPS PLACING " +
	"PRIMARY REFERENCES RETURNING RIGHT SELECT SESSION_USER SIMILAR SOME SYMMETRIC TABLE THEN TO " +
	"TRAILING TRUE UNION UNIQUE USER USING VARIADIC WHEN WHERE WINDOW WITH"

// cockroachDBReservedTables lists the keywords CockroachDB 26.4 refuses as a table's name
// in a definition, read as cockroachDBReservedColumns is.
const cockroachDBReservedTables = "ALL ANALYSE ANALYZE AND ANY ARRAY AS ASC ASYMMETRIC AUTHORIZATION BOTH CASE CAST CHECK " +
	"COLLATE COLLATION COLUMN CONCURRENTLY CONSTRAINT CREATE CROSS CURRENT_CATALOG CURRENT_DATE " +
	"CURRENT_ROLE CURRENT_SCHEMA CURRENT_TIME CURRENT_TIMESTAMP CURRENT_USER DEFAULT DEFERRABLE " +
	"DESC DISTINCT DO ELSE END EXCEPT FALSE FETCH FOR FOREIGN FROM FULL GRANT GROUP HAVING ILIKE " +
	"IN INITIALLY INNER INTERSECT INTO IS ISNULL JOIN LATERAL LEADING LEFT LIKE LIMIT LOCALTIME " +
	"LOCALTIMESTAMP NATURAL NONE NOT NOTNULL NULL OFFSET ON ONLY OR ORDER OUTER OVERLAPS PLACING " +
	"PRIMARY REFERENCES RETURNING RIGHT SELECT SESSION_USER SIMILAR SOME SYMMETRIC TABLE THEN TO " +
	"TRAILING TRUE UNION UNIQUE USER USING VARIADIC WHEN WHERE WINDOW WITH"
