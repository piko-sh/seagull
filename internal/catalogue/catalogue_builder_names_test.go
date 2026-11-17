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

package catalogue

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

func constraintNamesOf(constraints []sqlmodel.Constraint) []string {
	names := make([]string, 0, len(constraints))
	for _, constraint := range constraints {
		names = append(names, constraint.Name)
	}
	return names
}

func keyNamesOf(table *sqlmodel.Table) []string {
	names := constraintNamesOf(table.Constraints)
	for _, index := range table.Indexes {
		names = append(names, index.Name+" (index)")
	}
	slices.Sort(names)
	return names
}

func TestUnnamedConstraintsTakeTheEnginesNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine     dialect.DialectPort
		want       map[string][]string
		name       string
		migrations []string
	}{
		{
			name:   "PostgreSQL names keys, checks, foreign keys and indexes after the table and columns",
			engine: postgres.New(),
			migrations: []string{`CREATE TABLE t (id int PRIMARY KEY, a int UNIQUE, b int CHECK (b > 0), c int REFERENCES t (id), d int, e int,
				CHECK (d > e), CHECK (d > 0), UNIQUE (d, e), UNIQUE (a), CHECK (1 = 1), CHECK (b > 0 AND b < 10), FOREIGN KEY (c) REFERENCES t (id));
				CREATE INDEX ON t (a);
				CREATE INDEX ON t (a);
				CREATE UNIQUE INDEX ON t (d);
				CREATE INDEX ON t ((a + 1));
				CREATE INDEX ON t (lower(a::text));
				CREATE INDEX ON t (a, d);`},
			want: map[string][]string{"t": {"t_a_d_idx (index)", "t_a_idx (index)", "t_a_idx1 (index)", "t_a_key", "t_b_check", "t_b_check1",
				"t_c_fkey", "t_c_fkey1", "t_check", "t_check1", "t_d_check", "t_d_e_key", "t_d_idx (index)", "t_expr_idx (index)",
				"t_lower_idx (index)", "t_pkey"}},
		},
		{
			name:   "PostgreSQL keeps a name a rename leaves behind and numbers a name taken anywhere in the schema",
			engine: postgres.New(),
			migrations: []string{`CREATE TABLE u (x int, CHECK (x > 0));
				ALTER TABLE u ADD CHECK (x > 1);
				ALTER TABLE u ADD PRIMARY KEY (x);
				ALTER TABLE u RENAME TO u2;
				ALTER TABLE u2 ADD CHECK (x > 2);
				CREATE TABLE w (id int, CONSTRAINT w_pkey CHECK (id > 0));
				ALTER TABLE w ADD PRIMARY KEY (id);
				CREATE TABLE t (id int, a int UNIQUE);
				ALTER TABLE t ADD UNIQUE (a), ADD UNIQUE (a);`},
			want: map[string][]string{"u2": {"u2_x_check", "u_pkey", "u_x_check", "u_x_check1"}, "w": {"w_pkey", "w_pkey1"},
				"t": {"t_a_key", "t_a_key1", "t_a_key2"}},
		},
		{
			name:   "PostgreSQL makes one index of a unique key that repeats the primary key or another unique key",
			engine: postgres.New(),
			migrations: []string{`CREATE TABLE o (CHECK (a > 0), a int CHECK (a > 1), UNIQUE (a), b int UNIQUE, FOREIGN KEY (a) REFERENCES o (b),
				c int REFERENCES o (b), PRIMARY KEY (a));
				CREATE TABLE o2 (a int UNIQUE, UNIQUE (a), b int, UNIQUE (b), UNIQUE (b));
				CREATE TABLE o3 (a int, CHECK (o3.a > 0 AND length(a::text) > 0), CHECK (a > 0), CHECK ("A" > 0), "A" int);`},
			want: map[string][]string{"o": {"o_a_check", "o_a_check1", "o_a_fkey", "o_b_key", "o_c_fkey", "o_pkey"},
				"o2": {"o2_a_key", "o2_b_key"}, "o3": {"o3_A_check", "o3_a_check", "o3_a_check1"}},
		},
		{
			name:   "PostgreSQL cuts a name to 63 bytes, the longer part first",
			engine: postgres.New(),
			migrations: []string{`CREATE TABLE abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij_lo (
				colcolcolcolcolcolcolcolcolcol int PRIMARY KEY CHECK (colcolcolcolcolcolcolcolcolcol > 0));`},
			want: map[string][]string{"abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij_lo": {
				"abcdefghijabcdefghijabcdefgh_colcolcolcolcolcolcolcolcolc_check",
				"abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefgh_pkey"}},
		},
		{
			name:   "PostgreSQL names a partition's keys after the partition and keeps its parent's check and foreign key names",
			engine: postgres.New(),
			migrations: []string{`CREATE TABLE r (id int PRIMARY KEY);
				CREATE TABLE p (id int, a int, CONSTRAINT mypk PRIMARY KEY (id), CONSTRAINT myuq UNIQUE (id, a), CONSTRAINT mych CHECK (a > 0),
				FOREIGN KEY (a) REFERENCES r) PARTITION BY RANGE (id);
				CREATE TABLE p0 PARTITION OF p FOR VALUES FROM (0) TO (10);`},
			want: map[string][]string{"p": {"mych", "mypk", "myuq", "p_a_fkey"}, "p0": {"mych", "p0_id_a_key", "p0_pkey", "p_a_fkey"}},
		},
		{
			name:   "CockroachDB numbers names within the table and names checks after the columns they read",
			engine: cockroachdb.New(),
			migrations: []string{`CREATE TABLE t (id int PRIMARY KEY, a int UNIQUE, b int CHECK (b > 0), c int REFERENCES t (id), d int, e int,
				CHECK (d > e), CHECK (d > 0), UNIQUE (d, e), UNIQUE (a), CHECK (1 = 1), CHECK (b > 0 AND b < 10), FOREIGN KEY (c) REFERENCES t (id),
				INDEX (a), INDEX (a), UNIQUE INDEX (d));
				CREATE INDEX ON t (a);
				CREATE INDEX ON t ((a + 1), (a + 2));
				CREATE UNIQUE INDEX ON t (d);`},
			want: map[string][]string{"t": {"check", "check_b", "check_b_b", "check_d", "check_d_e", "t_a_idx (index)", "t_a_idx1 (index)",
				"t_a_idx2 (index)", "t_a_key", "t_a_key1", "t_c_fkey", "t_c_fkey_1", "t_d_e_key", "t_d_key", "t_d_key1 (index)",
				"t_expr_expr1_idx (index)", "t_pkey"}},
		},
		{
			name:   "CockroachDB keeps the key name a table had from its creation",
			engine: cockroachdb.New(),
			migrations: []string{`CREATE TABLE u (x int, CHECK (x > 0));
				ALTER TABLE u ADD CHECK (x > 1);
				ALTER TABLE u ADD UNIQUE (x);
				ALTER TABLE u RENAME TO u2;
				ALTER TABLE u2 ADD CHECK (x > 2);
				CREATE TABLE m4 (id int NOT NULL);
				ALTER TABLE m4 RENAME TO m5;
				ALTER TABLE m5 ADD PRIMARY KEY (id);`},
			want: map[string][]string{"u2": {"check_x", "check_x1", "check_x2", "u_pkey", "u_x_key"}, "m5": {"m4_pkey"}},
		},
		{
			name:   "MySQL names a key after its first column and numbers foreign keys and checks after the table",
			engine: mysql.New(),
			migrations: []string{`CREATE TABLE p (id int PRIMARY KEY, x int UNIQUE);
				CREATE TABLE t (id int PRIMARY KEY, a int UNIQUE, b int CHECK (b > 0), c int REFERENCES p (id), d int, e int, CHECK (d > e),
				UNIQUE (d, e), UNIQUE (a), UNIQUE KEY (a), UNIQUE KEY uk (e), CONSTRAINT cu UNIQUE (b), CONSTRAINT cv UNIQUE KEY ui (c),
				FOREIGN KEY (c) REFERENCES p (id), CONSTRAINT named_fk FOREIGN KEY (d) REFERENCES p (id), CHECK (1 = 1), INDEX (a), KEY (b));
				ALTER TABLE t ADD UNIQUE (a);
				ALTER TABLE t ADD FOREIGN KEY (e) REFERENCES p (id);
				ALTER TABLE t ADD CHECK (e > 0);
				RENAME TABLE t TO t2;
				ALTER TABLE t2 ADD FOREIGN KEY (a) REFERENCES p (id), ADD CHECK (a > 1);`},
			want: map[string][]string{"p": {"x"}, "t2": {"a", "a_2", "a_3", "a_4 (index)", "a_5", "b (index)", "cu", "d", "named_fk",
				"t2_chk_1", "t2_chk_2", "t2_chk_3", "t2_chk_4", "t2_chk_5", "t2_ibfk_1", "t2_ibfk_2", "t2_ibfk_3", "t2_ibfk_4", "ui", "uk"}},
		},
		{
			name:   "MySQL numbers past the highest generated number, and replaces a foreign key's index by a key on its columns",
			engine: mysql.New(),
			migrations: []string{`CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE t (id int PRIMARY KEY, a int, b int, c int, CHECK (a > 0), CHECK (a > 1), FOREIGN KEY (a) REFERENCES p (id),
				FOREIGN KEY (b) REFERENCES p (id), FOREIGN KEY (c) REFERENCES p (id));
				ALTER TABLE t DROP CHECK t_chk_1;
				ALTER TABLE t DROP FOREIGN KEY t_ibfk_1;
				ALTER TABLE t ADD CHECK (b > 0), ADD FOREIGN KEY (a) REFERENCES p (id), ADD UNIQUE (a), ADD UNIQUE (b);
				CREATE TABLE u (a int, CONSTRAINT u_ibfk_7 FOREIGN KEY (a) REFERENCES p (id), FOREIGN KEY (a) REFERENCES p (id), CHECK (a > 1));
				ALTER TABLE u ADD FOREIGN KEY (a) REFERENCES p (id);
				ALTER TABLE u RENAME TO w;`},
			want: map[string][]string{"p": nil, "t": {"a", "b", "c (index)", "t_chk_2", "t_chk_3", "t_ibfk_2", "t_ibfk_3", "t_ibfk_4"},
				"w": {"a (index)", "w_chk_1", "w_ibfk_1", "w_ibfk_7", "w_ibfk_8"}},
		},
		{
			name:   "MariaDB names a table's check CONSTRAINT_n, a column's check after the column and a foreign key by its number",
			engine: mariadb.New(),
			migrations: []string{`CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE t (id int PRIMARY KEY, b int CHECK (b > 0), c int REFERENCES p (id), CHECK (b > 1), CONSTRAINT CONSTRAINT_2 CHECK (b > 2),
				CHECK (b > 3), FOREIGN KEY (c) REFERENCES p (id));
				ALTER TABLE t DROP CONSTRAINT CONSTRAINT_1;
				ALTER TABLE t ADD CHECK (c > 0), ADD CHECK (c > 1);
				RENAME TABLE t TO t2;`},
			want: map[string][]string{"p": nil, "t2": {"1", "2", "CONSTRAINT_1", "CONSTRAINT_2", "CONSTRAINT_3", "CONSTRAINT_4", "b",
				"c (index)"}},
		},
		{
			name:   "MySQL makes a foreign key's index again for a later foreign key on its columns, unless the table indexes them",
			engine: mysql.New(),
			migrations: []string{"CREATE TABLE p (id int PRIMARY KEY);\n" +
				"CREATE TABLE w (id int PRIMARY KEY, a int, b int, c int, CONSTRAINT fka FOREIGN KEY (a) REFERENCES p (id), " +
				"FOREIGN KEY (a) REFERENCES p (id), CONSTRAINT fkb FOREIGN KEY (b) REFERENCES p (id));\n" +
				"ALTER TABLE w ADD FOREIGN KEY (a) REFERENCES p (id);\nALTER TABLE w ADD CONSTRAINT fkb2 FOREIGN KEY (b) REFERENCES p (id);\n" +
				"CREATE INDEX userx ON w (c);\nALTER TABLE w ADD FOREIGN KEY (c) REFERENCES p (id);"},
			want: map[string][]string{"w": {"a (index)", "fka", "fkb", "fkb2", "fkb2 (index)", "userx (index)", "w_ibfk_1", "w_ibfk_2",
				"w_ibfk_3"}},
		},
		{
			name:   "MariaDB makes a foreign key's index again for a later foreign key on its columns, unless the table indexes them",
			engine: mariadb.New(),
			migrations: []string{"CREATE TABLE p (id int PRIMARY KEY);\n" +
				"CREATE TABLE w (id int PRIMARY KEY, a int, b int, c int, CONSTRAINT fka FOREIGN KEY (a) REFERENCES p (id), " +
				"FOREIGN KEY (a) REFERENCES p (id), CONSTRAINT fkb FOREIGN KEY (b) REFERENCES p (id));\n" +
				"ALTER TABLE w ADD FOREIGN KEY (a) REFERENCES p (id);\nALTER TABLE w ADD CONSTRAINT fkb2 FOREIGN KEY (b) REFERENCES p (id);\n" +
				"CREATE INDEX userx ON w (c);\nALTER TABLE w ADD FOREIGN KEY (c) REFERENCES p (id);"},
			want: map[string][]string{"w": {"1", "2", "3", "a (index)", "fka", "fkb", "fkb2", "fkb2 (index)", "userx (index)"}},
		},
		{
			name:   "MariaDB numbers a foreign key past the highest number a name of the table holds and keeps it on a rename",
			engine: mariadb.New(),
			migrations: []string{"CREATE TABLE p (id int PRIMARY KEY);\n" +
				"CREATE TABLE w (id int PRIMARY KEY, a int, CONSTRAINT `5` FOREIGN KEY (a) REFERENCES p (id), FOREIGN KEY (a) REFERENCES p (id));\n" +
				"ALTER TABLE w ADD FOREIGN KEY (a) REFERENCES p (id);\n" +
				"CREATE TABLE x (id int PRIMARY KEY, a int, CONSTRAINT x_ibfk_3 FOREIGN KEY (a) REFERENCES p (id));\n" +
				"ALTER TABLE x ADD FOREIGN KEY (a) REFERENCES p (id);\n" +
				"CREATE TABLE t (id int PRIMARY KEY, a int, b int, c int REFERENCES p (id), FOREIGN KEY (a) REFERENCES p (id), " +
				"FOREIGN KEY (b) REFERENCES p (id));\n" +
				"ALTER TABLE t DROP FOREIGN KEY `1`;\nALTER TABLE t ADD FOREIGN KEY (a) REFERENCES p (id);\nRENAME TABLE t TO t2;"},
			want: map[string][]string{"p": nil, "w": {"1", "5", "6", "a (index)"}, "x": {"1", "a (index)", "x_ibfk_3"},
				"t2": {"2", "3", "4", "a (index)", "b (index)", "c (index)"}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := replayMigrationsWith(t, testCase.engine, testCase.migrations...)
			for name, want := range testCase.want {
				table := catalogue.LookupTable("", name)
				require.NotNil(t, table, name)
				if want == nil {
					want = []string{}
				}
				require.Equal(t, want, keyNamesOf(table), name)
			}
		})
	}
}

func TestDroppingAConstraintByTheEnginesNameDropsIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine         dialect.DialectPort
		name           string
		migrations     []string
		wantNames      []string
		wantPrimaryKey []string
	}{
		{
			name:   "PostgreSQL",
			engine: postgres.New(),
			migrations: []string{
				"CREATE TABLE p (id int PRIMARY KEY); CREATE TABLE t (id int PRIMARY KEY, a int UNIQUE CHECK (a > 0) REFERENCES p (id), b int);",
				"CREATE INDEX ON t (b);",
				"ALTER TABLE t DROP CONSTRAINT t_pkey, DROP CONSTRAINT t_a_key, DROP CONSTRAINT t_a_check, DROP CONSTRAINT t_a_fkey;",
				"DROP INDEX t_b_idx;",
			},
			wantNames: []string{}, wantPrimaryKey: nil,
		},
		{
			name:   "CockroachDB",
			engine: cockroachdb.New(),
			migrations: []string{
				"CREATE TABLE p (id int PRIMARY KEY); CREATE TABLE t (id int PRIMARY KEY, a int UNIQUE CHECK (a > 0) REFERENCES p (id), b int, INDEX (b));",
				"DROP INDEX t@t_a_key CASCADE; DROP INDEX t@t_b_idx;",
				"ALTER TABLE t DROP CONSTRAINT check_a, DROP CONSTRAINT t_a_fkey;",
			},
			wantNames: []string{"t_pkey"}, wantPrimaryKey: []string{"id"},
		},
		{

			name:   "CockroachDB unique indexes",
			engine: cockroachdb.New(),
			migrations: []string{
				"CREATE TABLE t (id int PRIMARY KEY, d int, e int, UNIQUE INDEX (e));",
				"CREATE UNIQUE INDEX ON t (d);",
				"ALTER TABLE t DROP CONSTRAINT t_d_key;",
				"ALTER TABLE t DROP CONSTRAINT t_e_key;",
			},
			wantNames: []string{"t_pkey"}, wantPrimaryKey: []string{"id"},
		},
		{
			name:   "MySQL",
			engine: mysql.New(),
			migrations: []string{
				"CREATE TABLE p (id int PRIMARY KEY); CREATE TABLE t (id int PRIMARY KEY, a int UNIQUE CHECK (a > 0), b int, FOREIGN KEY (b) REFERENCES p (id));",
				"ALTER TABLE t DROP INDEX a, DROP CHECK t_chk_1, DROP FOREIGN KEY t_ibfk_1, DROP INDEX b;",
				"ALTER TABLE t DROP CONSTRAINT `PRIMARY`;",
			},
			wantNames: []string{}, wantPrimaryKey: nil,
		},
		{

			name:   "MariaDB",
			engine: mariadb.New(),
			migrations: []string{
				"CREATE TABLE p (id int PRIMARY KEY); CREATE TABLE t (id int PRIMARY KEY, a int UNIQUE CHECK (a > 0), b int REFERENCES p (id), CHECK (b > 0));",
				"ALTER TABLE t DROP CONSTRAINT a, DROP CONSTRAINT CONSTRAINT_1, DROP FOREIGN KEY `1`;",
				"DROP INDEX `PRIMARY` ON t;",
			},
			wantNames: []string{"a", "b (index)"}, wantPrimaryKey: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := replayMigrationsWith(t, testCase.engine, testCase.migrations...)
			table := catalogue.LookupTable("", "t")
			require.NotNil(t, table)
			require.Equal(t, testCase.wantNames, keyNamesOf(table), strings.Join(testCase.migrations, "\n"))
			require.Equal(t, testCase.wantPrimaryKey, table.PrimaryKey)
		})
	}
}
