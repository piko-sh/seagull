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

import (
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// defaultHashShardBuckets is the number of buckets CockroachDB shards a hash-sharded
	// index into when the index gives no bucket_count.
	defaultHashShardBuckets = 16

	// hashShardPrefix opens the name of the hidden column holding a row's shard.
	hashShardPrefix = "crdb_internal_"

	// hashShardSuffix follows the key columns in the name of the hidden shard column, before
	// the bucket count.
	hashShardSuffix = "_shard_"
)

// addHashShardColumn gives a table whose primary key is hash-sharded, as in PRIMARY KEY
// (ts, id) USING HASH, the hidden column CockroachDB adds to hold each row's shard:
// crdb_internal_ts_id_shard_16, an INT8 computed from the key, which leads the primary
// key and which SELECT * leaves out.
//
// Takes mutation (*ast.CatalogueMutation) which is the parsed statement's mutation; one
// that creates no table, or a table whose primary key the postgres parser did not record
// as hash-sharded, is left alone.
//
// Returns error which is always nil.
func addHashShardColumn(_ pgext.ParserContext, _ pgext.StatementKind, mutation *ast.CatalogueMutation) error {
	if mutation == nil || mutation.Kind != ast.MutationCreateTable {
		return nil
	}
	written, sharded := mutation.EngineSpecific[pgext.EngineSpecificHashShardBuckets]
	if !sharded || len(mutation.PrimaryKey) == 0 {
		return nil
	}
	name := hashShardPrefix + strings.Join(mutation.PrimaryKey, "_") + hashShardSuffix + strconv.Itoa(bucketCount(written))
	shard := sqlmodel.NewColumn(name, sqlmodel.NewIntegerType(engineNameInt8, widthInt8, false), false)
	shard.Hidden = true
	shard.IsGenerated = true
	shard.GeneratedKind = sqlmodel.GeneratedKindVirtual
	mutation.Columns = append(mutation.Columns, shard)
	mutation.PrimaryKey = append([]string{name}, mutation.PrimaryKey...)
	return nil
}

// bucketCount reads the bucket count a hash-sharded key's bucket_count storage parameter
// gives.
//
// Takes written (string) which is the parameter's value as written, or empty when the key
// gives none.
//
// Returns int which is the bucket count, or the default when the value is missing or not
// a positive whole number.
func bucketCount(written string) int {
	if buckets, err := strconv.Atoi(written); err == nil && buckets > 0 {
		return buckets
	}
	return defaultHashShardBuckets
}
