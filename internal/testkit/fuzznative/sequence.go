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

package fuzznative

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// SequenceResponse records one sequence model's observations.
//
// Builder sequences (version 16) and typed value sequences (version 15) share it, so one
// helper protocol serves both.
type SequenceResponse struct {
	// Stage names the operation that failed before completion.
	Stage string

	// Error describes a failure outside the observed calls, without connection details.
	Error string

	// Panic records a recovered panic, which always fails the input.
	Panic string

	// Observations holds one JSON-encoded observation per modelled call.
	Observations json.RawMessage

	// ID matches the request being answered.
	ID uint64

	// Version identifies the response schema.
	Version int
}

// SequenceRunner runs one decoded request against a generated API.
//
// Takes context.Context which bounds the generated calls.
// Takes *Session which owns the generated API.
// Takes []byte which holds the versioned model input.
//
// Returns any which is the JSON-encodable observation list.
// Returns error when the input does not decode.
type SequenceRunner func(context.Context, *Session, []byte) (any, error)

// envelope returns the answered request ID and protocol version.
//
// Returns uint64 which is the request ID.
// Returns int which is the protocol version.
func (response *SequenceResponse) envelope() (uint64, int) { return response.ID, response.Version }

// redact removes the connection string from the failure text.
//
// Takes connection (string) which is the private connection string.
func (response *SequenceResponse) redact(connection string) {
	response.Error = redactConnection(response.Error, connection)
	response.Panic = redactConnection(response.Panic, connection)
}

// ExchangeSequence runs one sequence input and validates the response envelope.
// A worker must call ExchangeSequence and Close sequentially on a process.
//
// Takes connection (string) which names the isolated database.
// Takes data ([]byte) which holds the versioned model input.
//
// Returns SequenceResponse which belongs to this request.
// Returns error when the helper fails, ignores cancellation or violates the protocol.
func (process *Process) ExchangeSequence(ctx context.Context, connection string, data []byte) (SequenceResponse, error) {
	id := process.next()
	request := Request{Connection: connection, Data: data, ID: id, Version: ProtocolVersion}
	return exchange[SequenceResponse](ctx, process, request, connection, id, ProtocolVersion)
}

// ServeSequences handles requests for a helper compiled over a sequence catalogue.
//
// Takes factory (Factory) which constructs the generated API.
// Takes runner (SequenceRunner) which decodes and runs one input.
// Takes input (io.Reader) which supplies request frames.
// Takes output (io.Writer) which receives response frames.
//
// Returns error when the protocol is invalid or a response cannot be written.
func ServeSequences(factory Factory, runner SequenceRunner, input io.Reader, output io.Writer) error {
	return serveFrames(input, output, func(request *Request) (any, error) {
		if request.Version != ProtocolVersion || request.ID == 0 {
			return nil, errInvalidEnvelope
		}
		return serveSequence(factory, runner, request), nil
	})
}

// serveSequence owns a native connection for one sequence.
//
// Takes factory (Factory) which constructs the generated API.
// Takes runner (SequenceRunner) which runs the input.
// Takes request (*Request) which holds the connection and input.
//
// Returns SequenceResponse which reports failures without the connection string.
func serveSequence(factory Factory, runner SequenceRunner, request *Request) (response SequenceResponse) {
	response.ID, response.Version = request.ID, ProtocolVersion
	defer func() {
		if recovered := recover(); recovered != nil {
			response.Panic = fmt.Sprint(recovered)
		}
		response.redact(request.Connection)
	}()
	ctx, cancel := context.WithTimeoutCause(context.Background(), nativeInputBound, errInputBound)
	defer cancel()
	response.Stage = "factory"
	session, err := factory(ctx, request.Connection)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	if !session.complete() {
		response.Error = errIncompleteSession.Error()
		return response
	}
	defer func() {
		if err := session.Close(); err != nil && response.Error == "" {
			response.Stage, response.Error = "close", err.Error()
		}
	}()
	response.Stage = "run"
	observations, err := runner(ctx, session, request.Data)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	response.Observations, err = json.Marshal(observations)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	response.Stage = ""
	return response
}
