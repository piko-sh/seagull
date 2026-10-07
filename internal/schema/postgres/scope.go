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
	// coveredSchemas selects the names of the schemas a catalogue covers: those the role may
	// use, leaving out the system schemas (information_schema, every pg_ schema including
	// pg_toast and the temporary ones, CockroachDB's crdb_internal), TimescaleDB's internal
	// schemas that hold chunks and catalogue tables, and any schema an extension created.
	// Every catalogue query filters by it, so each runs once for all the schemas rather than
	// once per schema.
	coveredSchemas = `SELECT covered.nspname
		 FROM pg_catalog.pg_namespace covered
		 WHERE covered.nspname NOT IN ('information_schema', 'crdb_internal')
		 AND covered.nspname NOT LIKE 'pg\_%'
		 AND covered.nspname NOT LIKE '\_timescaledb%'
		 AND pg_catalog.has_schema_privilege(covered.oid, 'USAGE')
		 AND NOT EXISTS (
			SELECT 1 FROM pg_catalog.pg_depend dependency
			WHERE dependency.classid = 'pg_catalog.pg_namespace'::regclass
			AND dependency.objid = covered.oid AND dependency.deptype = 'e'
		 )`

	// schemasQuery lists the schemas a catalogue covers, as coveredSchemas selects them.
	schemasQuery = coveredSchemas + `
		 ORDER BY covered.nspname`

	// inCoveredSchema keeps only rows whose namespace, aliased n, is a covered schema.
	inCoveredSchema = `n.nspname IN (` + coveredSchemas + `)`

	// notExtensionRelation keeps only relations, aliased c, that no extension owns.
	notExtensionRelation = `NOT EXISTS (
			SELECT 1 FROM pg_catalog.pg_depend dependency
			WHERE dependency.classid = 'pg_catalog.pg_class'::regclass
			AND dependency.objid = c.oid AND dependency.deptype = 'e'
		 )`

	// notExtensionType keeps only types, aliased t, that no extension owns.
	notExtensionType = `NOT EXISTS (
			SELECT 1 FROM pg_catalog.pg_depend dependency
			WHERE dependency.classid = 'pg_catalog.pg_type'::regclass
			AND dependency.objid = t.oid AND dependency.deptype = 'e'
		 )`

	// tablesQuery lists the covered schemas' tables and their partition parents, excluding
	// relations owned by extensions.
	tablesQuery = `SELECT n.nspname, c.relname, COALESCE(parent_namespace.nspname, ''), COALESCE(parent.relname, '')
		 FROM pg_catalog.pg_class c
		 JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		 LEFT JOIN pg_catalog.pg_inherits inheritance
			ON inheritance.inhrelid = c.oid AND COALESCE(c.relispartition, false)
		 LEFT JOIN pg_catalog.pg_class parent ON parent.oid = inheritance.inhparent
		 LEFT JOIN pg_catalog.pg_namespace parent_namespace ON parent_namespace.oid = parent.relnamespace
		 WHERE ` + inCoveredSchema + ` AND c.relkind IN ('r', 'p', 'f')
		 AND ` + notExtensionRelation + `
		 ORDER BY n.nspname, c.relname`

	// viewsQuery lists the covered schemas' views and materialised views with their
	// definitions, leaving out those an extension owns.
	viewsQuery = `SELECT n.nspname, c.relname, pg_catalog.pg_get_viewdef(c.oid)
		 FROM pg_catalog.pg_class c
		 JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		 WHERE ` + inCoveredSchema + ` AND c.relkind IN ('v', 'm')
		 AND ` + notExtensionRelation + `
		 ORDER BY n.nspname, c.relname`

	// columnsQuery lists the covered schemas' table and view columns in declaration order
	// with their nullability, default, identity and generation flags, and type description.
	columnsQuery = `SELECT n.nspname,
			c.relname,
			a.attname,
			a.attnotnull,
			(attribute_default.oid IS NOT NULL AND a.attgenerated::text = ''),
			a.attidentity::text <> '',
			a.attgenerated::text,` + attributeTypeColumns + `
		 FROM pg_catalog.pg_attribute a
		 JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		 JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace` + attributeTypeJoins + `
		 LEFT JOIN pg_catalog.pg_attrdef attribute_default
			ON attribute_default.adrelid = a.attrelid AND attribute_default.adnum = a.attnum
		 WHERE ` + inCoveredSchema + ` AND c.relkind IN ('v', 'm', 'r', 'p', 'f')
		 AND ` + notExtensionRelation + ` AND a.attnum > 0 AND NOT a.attisdropped
		 ORDER BY n.nspname, c.relname, a.attnum`

	// constraintsQuery lists the key columns of the covered schemas' primary key and unique
	// constraints, in key order.
	constraintsQuery = `SELECT n.nspname, t.relname, con.conname, con.contype, att.attname, un.ord
		 FROM pg_constraint con
		 JOIN pg_class t ON t.oid = con.conrelid
		 JOIN pg_namespace n ON n.oid = t.relnamespace
		 JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS un(attnum, ord) ON true
		 JOIN pg_attribute att ON att.attrelid = t.oid AND att.attnum = un.attnum
		 WHERE ` + inCoveredSchema + ` AND con.contype IN ('p', 'u')
		 ORDER BY n.nspname, t.relname, con.conname, un.ord`

	// indexesQuery lists the simple-column members of the covered schemas' indexes in their
	// key order.
	indexesQuery = `SELECT n.nspname, t.relname, i.relname AS index_name,
			ix.indisunique, ix.indisprimary, a.attname, array_position(ix.indkey, a.attnum) AS column_position
		 FROM pg_index ix
		 JOIN pg_class i ON i.oid = ix.indexrelid
		 JOIN pg_class t ON t.oid = ix.indrelid
		 JOIN pg_namespace n ON n.oid = t.relnamespace
		 JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
		 WHERE ` + inCoveredSchema + `
		 ORDER BY n.nspname, t.relname, i.relname, array_position(ix.indkey, a.attnum)`

	// enumsQuery lists the labels of the covered schemas' enums in their sort order.
	enumsQuery = `SELECT n.nspname, t.typname, e.enumlabel
		 FROM pg_type t
		 JOIN pg_enum e ON t.oid = e.enumtypid
		 JOIN pg_namespace n ON t.typnamespace = n.oid
		 WHERE ` + inCoveredSchema + `
		 ORDER BY n.nspname, t.typname, e.enumsortorder`

	// hiddenColumnsSupportQuery counts the attishidden column of pg_attribute, which
	// CockroachDB has and PostgreSQL lacks: one when the server can hide a column from
	// SELECT *, as CockroachDB hides the rowid it gives a table without a primary key.
	hiddenColumnsSupportQuery = `SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'pg_catalog' AND table_name = 'pg_attribute' AND column_name = 'attishidden'`

	// hiddenColumnsQuery lists the schema, relation and name of every live column the server
	// hides from SELECT *.
	hiddenColumnsQuery = `SELECT n.nspname, c.relname, a.attname
		 FROM pg_catalog.pg_attribute a
		 JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		 JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		 WHERE a.attnum > 0 AND NOT a.attisdropped AND a.attishidden`

	// compositeTypesQuery lists the fields of the covered schemas' standalone composite
	// types (CREATE TYPE ... AS), leaving out the row types every table, view and foreign
	// table carries and the composites an extension owns.
	compositeTypesQuery = `SELECT n.nspname, t.typname, a.attname,` + attributeTypeColumns + `
		 FROM pg_catalog.pg_type t
		 JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
		 JOIN pg_catalog.pg_class c ON c.oid = t.typrelid AND c.relkind = 'c'
		 JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid` + attributeTypeJoins + `
		 WHERE t.typtype = 'c' AND ` + inCoveredSchema + ` AND a.attnum > 0 AND NOT a.attisdropped
		 AND ` + notExtensionType + `
		 ORDER BY n.nspname, t.typname, a.attnum`

	// extensionsQuery lists the installed extensions, leaving out plpgsql, which every
	// database has.
	extensionsQuery = `SELECT extname FROM pg_extension WHERE extname != 'plpgsql'`
)
