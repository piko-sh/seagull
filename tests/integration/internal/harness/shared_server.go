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

package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SharedServer lets fuzz workers attach to their coordinator's disposable server.
//
// Takes engine (string) which names the suite.
// Takes connection (*string) which receives the endpoint from start or the coordinator.
// Takes start (Starter) which starts a server owned by this process.
//
// Returns Starter which starts once in the coordinator and attaches in its workers.
func SharedServer(engine string, connection *string, start Starter) Starter {
	return func(ctx context.Context) (func(context.Context) error, error) {
		key := "SEAGULL_TEST_SERVER_" + strings.ToUpper(engine)
		ownerKey := key + "_OWNER"
		owner := os.Getenv(ownerKey)
		if owner != "" {
			return attachSharedServer(engine, key, owner, connection)
		}
		stop, err := start(ctx)
		if err != nil {
			return nil, err
		}
		cleanup := func(ctx context.Context) error {
			var stopErr error
			if stop != nil {
				stopErr = stop(ctx)
			}
			return errors.Join(stopErr, os.Unsetenv(key), os.Unsetenv(ownerKey))
		}
		if err := os.Setenv(key, *connection); err != nil {
			return nil, errors.Join(err, cleanup(context.WithoutCancel(ctx)))
		}
		if err := os.Setenv(ownerKey, strconv.Itoa(os.Getpid())); err != nil {
			return nil, errors.Join(err, cleanup(context.WithoutCancel(ctx)))
		}
		return cleanup, nil
	}
}

// attachSharedServer validates ownership before attaching a worker.
//
// Takes engine (string) which identifies the suite.
// Takes key (string) which names the inherited endpoint variable.
// Takes owner (string) which identifies the coordinator process.
// Takes connection (*string) which receives the validated endpoint.
//
// Returns func(context.Context) error which is nil because workers own no server.
// Returns error when the endpoint is stale or incomplete.
func attachSharedServer(engine, key, owner string, connection *string) (func(context.Context) error, error) {
	if owner != strconv.Itoa(os.Getppid()) {
		return nil, fmt.Errorf("%s endpoint was not inherited from the parent coordinator", engine)
	}
	*connection = os.Getenv(key)
	if *connection == "" {
		return nil, fmt.Errorf("%s coordinator supplied no endpoint", engine)
	}
	return nil, nil
}
