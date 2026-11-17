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

import "seagull.sh/seagull/internal/dialect/dialectkit"

// sqliteReservedColumns lists the keywords SQLite refuses as a column's name in a
// definition, read by preparing CREATE TABLE with each keyword as a column.
const sqliteReservedColumns = "ADD ALL ALTER AND AS AUTOINCREMENT BETWEEN CASE CHECK COLLATE COMMIT CREATE DEFAULT " +
	"DEFERRABLE DELETE DISTINCT DROP ELSE ESCAPE EXCEPT EXISTS FOREIGN FROM GROUP HAVING IN INDEX " +
	"INSERT INTERSECT INTO IS ISNULL JOIN LIMIT NOT NOTHING NOTNULL NULL ON OR ORDER PRIMARY " +
	"REFERENCES RETURNING SELECT SET TABLE THEN TO TRANSACTION UNION UNIQUE UPDATE USING VALUES " +
	"WHEN WHERE"

// sqliteReservedTables lists the keywords SQLite refuses as a table's name in a
// definition, read as sqliteReservedColumns is.
const sqliteReservedTables = "ADD ALL ALTER AND AS AUTOINCREMENT BETWEEN CASE CHECK COLLATE COMMIT CONSTRAINT CREATE " +
	"DEFAULT DEFERRABLE DELETE DISTINCT DROP ELSE ESCAPE EXCEPT EXISTS FOREIGN FROM GROUP HAVING " +
	"IF IN INDEX INSERT INTERSECT INTO IS ISNULL JOIN LIMIT NOT NOTHING NOTNULL NULL ON OR ORDER " +
	"PRIMARY REFERENCES RETURNING SELECT SET TABLE THEN TO TRANSACTION UNION UNIQUE UPDATE USING " +
	"VALUES WHEN WHERE"

// sqliteReservedNames holds both lists, built once so each parser shares them.
var sqliteReservedNames = dialectkit.NewReservedNames(sqliteReservedColumns, sqliteReservedTables)
