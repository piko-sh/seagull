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

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"case_runner/db"
)

const schema = `CREATE TABLE things (
  id INTEGER PRIMARY KEY,
  flag TEXT NOT NULL,
  doc TEXT NOT NULL,
  label TEXT NOT NULL,
  amount TEXT NOT NULL
);
INSERT INTO things (id, flag, doc, label, amount) VALUES (1, 'true', '{"a":1}', '12abc', '2.5');`

func main() {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	output := map[string]any{}
	casts, err := db.New(conn).ThingCasts(ctx, 1)
	if err != nil {
		output["casts"] = "error: " + err.Error()
	} else {
		output["casts"] = map[string]any{
			"flag_number": casts.FlagNumber.String(), "amount_number": casts.AmountNumber.String(),
			"doc_number": casts.DocNumber.String(), "binary_doc_number": casts.BinaryDocNumber.String(),
			"label_number": casts.LabelNumber.String(), "any_number": casts.AnyNumber.String(),
			"label_text": casts.LabelText, "whole": casts.Whole,
		}
	}

	for name, wanted := range map[string]bool{"things_when_true": true, "things_when_false": false} {
		things, err := db.New(conn).ThingsWhen(ctx, wanted)
		if err != nil {
			output[name] = "error: " + err.Error()
			continue
		}
		output[name] = things
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
