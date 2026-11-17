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

package fuzzcase

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyDecoderCompatibility(t *testing.T) {
	for engine, want := range map[string]string{
		"postgres": "370f8aaf9593c7c37f75dd3ab4e254fd83bc8332c6c55c1960fb05756982b4d1", "sqlite": "4605f614a02a89d51db599bb7d62cfcd53b09b6d6fadc0270f0ce5afd7d016fe", "mysql": "fc3985b38a2d5e4896a9cdbb63a357078a01fdd047a3bbb502c337aaa62f96c9", "mariadb": "5c3454bcb6fbcfbe64423e3642ea06026685710afe79da6bd7f59a5cf66ff968",
		"duckdb": "2ee48eb03fc204581770d504f0dc48b18416a76f08f37b2cb46c4c155ac5b97d", "clickhouse": "76754821032cc3a2a3d9134c439473b963b3d4f48ce5cbbc7d31a87e325257c7", "cockroachdb": "9834ee216247f5ed4c3183c40dd23ba446bd87cab56b016c2802fdd32750dddb", "timescaledb": "5fbcfa901e723c7079cae5a120d1c24d7f652e32ec20315da11fd9b60da80725",
	} {
		t.Run(engine, func(t *testing.T) {
			hash := sha256.New()
			encoder := json.NewEncoder(hash)
			encode := func(value any) {
				t.Helper()
				require.NoError(t, encoder.Encode(value))
			}
			inputs := append(Seeds(engine), MigrationSeeds(engine)...)
			inputs = append(inputs, RejectionSeeds()...)
			random := rand.New(rand.NewPCG(17, 29))
			for _, version := range []byte{1, 2, 3, 4, 5, 6, 7, 128} {
				for size := range 64 {
					data := make([]byte, size+1)
					data[0] = version
					for index := 1; index < len(data); index++ {
						data[index] = byte(random.UintN(256))
					}
					inputs = append(inputs, data)
				}
			}
			for _, data := range inputs {
				encode(data)
				model, ok := Decode(engine, data)
				encode(ok)
				if ok {
					encode(model.Migrations())
					encode(model.Inserts())
					encode(model.Query())
					encode(model.Variants())
					encode(model.Features())
					encode(model.CountWrapped())
				}
				migration, accepted := DecodeMigration(engine, data)
				encode(accepted)
				if accepted {
					encode(migration)
				}
				rejected, accepted := DecodeRejection(engine, data)
				encode(accepted)
				if accepted {
					encode(rejected)
				}
			}
			got := hex.EncodeToString(hash.Sum(nil))
			assert.Equal(t, want, got, "legacy decoder fingerprint for %s", engine)
		})
	}
}
