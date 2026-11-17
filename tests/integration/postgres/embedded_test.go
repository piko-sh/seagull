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

//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

const (
	startAttempts = 3
	startTimeout  = 120 * time.Second
	serverVersion = embeddedpostgres.PostgresVersion("18.7.0")
)

var testConnectionString string

var errNoTCPPort = errors.New("the port probe did not listen on TCP")

func startEmbeddedPostgres(ctx context.Context) (func(context.Context) error, error) {
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	var lastError error
	for range startAttempts {
		stop, err := startPostgresAttempt(ctx)
		if err == nil {
			return stop, nil
		}
		lastError = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("starting embedded postgres: %w", lastError)
}

func startPostgresAttempt(ctx context.Context) (func(context.Context) error, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp("", "sgl-integration-pg-")
	if err != nil {
		return nil, fmt.Errorf("creating the runtime directory: %w", err)
	}
	timeout := startTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	database := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().Version(serverVersion).
			Username("sgl_test").Password("sgl_test").Database("sgl_test").
			Port(port).RuntimePath(directory).StartTimeout(timeout),
	)
	if err := database.Start(); err != nil {
		return nil, errors.Join(err, database.Stop(), os.RemoveAll(directory))
	}
	testConnectionString = fmt.Sprintf("postgres://sgl_test:sgl_test@localhost:%d/sgl_test?sslmode=disable", port)
	return func(context.Context) error { return errors.Join(database.Stop(), os.RemoveAll(directory)) }, nil
}

func freePort() (uint32, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("probing for a free port: %w", err)
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.Join(errNoTCPPort, listener.Close())
	}
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("releasing the probed port: %w", err)
	}
	return uint32(address.Port), nil
}
