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

//go:build linux || darwin

package reduce

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"seagull.sh/seagull/cmd/internal/fuzztool/fuzztext"
)

const (
	// ReservedPrefix names every corpus entry the reducer writes.
	ReservedPrefix = "sgl-reduce-"

	// maxReplayOutput bounds the output kept from one replay.
	maxReplayOutput = 16 << 20

	// pipeGrace bounds the wait for output after a replay's process exits.
	pipeGrace = 5 * time.Second

	// millisecondsPerSecond scales attempt timings to three decimal places.
	millisecondsPerSecond = 1000

	// outcomeFailed, outcomePassed, outcomeOther and outcomeTimeout classify a replay.
	outcomeFailed = "failed"

	// outcomePassed is a replay that passed.
	outcomePassed = "passed"

	// outcomeOther is a replay that failed without a target failure.
	outcomeOther = "other"

	// outcomeTimeout is a replay that ran past its timeout.
	outcomeTimeout = "timeout"

	// corpusMode is the permission of a written corpus entry.
	corpusMode = 0o600

	// corpusDirectoryMode is the permission of a created corpus directory.
	corpusDirectoryMode = 0o750
)

// interruption reports an external signal that stopped the reduction.
type interruption struct {
	// signal is the signal received.
	signal os.Signal
}

// replayer replays inputs through a built test binary from the package's seed corpus.
type replayer struct {
	// log receives one JSON line per attempt.
	log io.Writer

	// signals delivers external interrupts.
	signals <-chan os.Signal

	// written holds every corpus file not yet removed.
	written map[string]bool

	// binary is the built test binary.
	binary string

	// corpusDirectory is the target's seed corpus.
	corpusDirectory string

	// packageDirectory is where the binary runs.
	packageDirectory string

	// function is the fuzz target.
	function string

	// created holds the directories made for the corpus, deepest first.
	created []string

	// runTimeout bounds one replay.
	runTimeout time.Duration

	// counter numbers the attempts.
	counter int
}

// attemptRecord is one line of attempts.jsonl.
type attemptRecord struct {
	// Outcome classifies the replay.
	Outcome string `json:"outcome"`

	// Identity is the failure's identity, null when it did not fail the target.
	Identity []string `json:"identity"`

	// Seconds is the replay's duration.
	Seconds jsonFloat `json:"seconds"`

	// Attempt numbers the replay.
	Attempt int `json:"attempt"`

	// Bytes is the size of the replayed corpus file.
	Bytes int `json:"bytes"`
}

// Error describes the interruption.
//
// Returns string which names the signal.
func (stopped *interruption) Error() string {
	return "interrupted by " + stopped.signal.String()
}

// newReplayer prepares replays from the package's seed corpus.
//
// Takes binary (string) which is the built test binary.
// Takes packageDirectory (string) which is where the binary runs.
// Takes function (string) which is the fuzz target.
// Takes runTimeout (time.Duration) which bounds one replay.
// Takes log (io.Writer) which receives the attempt log.
// Takes signals (<-chan os.Signal) which delivers external interrupts.
//
// Returns *replayer which is ready to replay.
func newReplayer(binary string, packageDirectory string, function string, runTimeout time.Duration, log io.Writer, signals <-chan os.Signal) *replayer {
	corpusDirectory := filepath.Join(packageDirectory, "testdata", "fuzz", function)
	var created []string
	for directory := corpusDirectory; ; directory = filepath.Dir(directory) {
		if _, err := os.Stat(directory); err == nil || filepath.Dir(directory) == directory {
			break
		}
		created = append(created, directory)
	}
	return &replayer{
		log:              log,
		signals:          signals,
		written:          make(map[string]bool),
		binary:           binary,
		corpusDirectory:  corpusDirectory,
		packageDirectory: packageDirectory,
		function:         function,
		created:          created,
		runTimeout:       runTimeout,
		counter:          0,
	}
}

// run replays one corpus file and returns its failure identity.
//
// Takes content ([]byte) which is the corpus file.
//
// Returns Identity which identifies the failure.
// Returns bool which is false when the replay did not fail the target.
// Returns error when the corpus file cannot be written or a signal stops the replay.
func (replay *replayer) run(content []byte) (Identity, bool, error) {
	replay.counter++
	name := ReservedPrefix + strconv.Itoa(os.Getpid()) + "-" + strconv.Itoa(replay.counter)
	path := filepath.Join(replay.corpusDirectory, name)
	if err := os.MkdirAll(replay.corpusDirectory, corpusDirectoryMode); err != nil {
		return Identity{}, false, fmt.Errorf("creating the corpus directory: %w", err)
	}
	replay.written[path] = true
	if err := os.WriteFile(path, content, corpusMode); err != nil {
		return Identity{}, false, fmt.Errorf("writing a candidate: %w", err)
	}
	started := time.Now()
	identity, failed, outcome, err := replay.replay(name)
	if removeErr := os.Remove(path); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
		delete(replay.written, path)
	}
	if err != nil {
		return Identity{}, false, err
	}
	record := attemptRecord{
		Outcome:  outcome,
		Identity: nil,
		Seconds:  roundedSeconds(time.Since(started), millisecondsPerSecond),
		Attempt:  replay.counter,
		Bytes:    len(content),
	}
	if failed {
		record.Identity = identity.list()
	}
	encoded, err := fuzztext.MarshalASCII(record, false)
	if err != nil {
		return Identity{}, false, err
	}
	if _, err := replay.log.Write(append(encoded, '\n')); err != nil {
		return Identity{}, false, fmt.Errorf("writing the attempt log: %w", err)
	}
	return identity, failed, nil
}

// replay runs the binary on one corpus entry, ending its whole process group on a
// timeout or a signal.
//
// Takes name (string) which is the corpus entry.
//
// Returns Identity which identifies the failure.
// Returns bool which is true when the replay failed the target.
// Returns string which classifies the replay.
// Returns error when the binary cannot start or a signal stops the replay.
//
// Concurrency: replay waits for the process in a goroutine and consumes the wait
// result before returning, including after a timeout or signal.
func (replay *replayer) replay(name string) (Identity, bool, string, error) {
	command := exec.Command( //nolint:gosec // Runs the binary the reducer built.
		replay.binary, "-test.run=^"+replay.function+"$/^"+name+"$", "-test.count=1", "-test.v")
	command.Dir = replay.packageDirectory
	output := &boundedBuffer{buffer: bytes.Buffer{}}
	command.Stdout = output
	command.Stderr = output
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.WaitDelay = pipeGrace
	if err := command.Start(); err != nil {
		return Identity{}, false, "", fmt.Errorf("starting the test binary: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	timer := time.NewTimer(replay.runTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		killGroup(command, done)
		return Identity{}, false, outcomeTimeout, nil
	case signal := <-replay.signals:
		killGroup(command, done)
		return Identity{}, false, "", &interruption{signal: signal}
	}
	if command.ProcessState.Success() {
		return Identity{}, false, outcomePassed, nil
	}
	identity, failed := identify(replay.function, output.buffer.String())
	if !failed {
		return Identity{}, false, outcomeOther, nil
	}
	return identity, true, outcomeFailed, nil
}

// reproduces reports whether an input fails with the wanted identity every time.
//
// Takes content ([]byte) which is the corpus file.
// Takes wanted (Identity) which is the original failure.
// Takes times (int) which is the number of replays.
//
// Returns bool which is true when every replay failed the same way.
// Returns error when a replay cannot run.
func (replay *replayer) reproduces(content []byte, wanted Identity, times int) (bool, error) {
	for range times {
		identity, failed, err := replay.run(content)
		if err != nil || !failed || identity != wanted {
			return false, err
		}
	}
	return true, nil
}

// cleanup removes every corpus file this run wrote and the directories made for them.
func (replay *replayer) cleanup() {
	for path := range replay.written {
		_ = os.Remove(path)
		delete(replay.written, path)
	}
	for _, directory := range replay.created {

		_ = os.Remove(directory)
	}
}

// boundedBuffer keeps the first maxReplayOutput bytes of a replay's output.
type boundedBuffer struct {
	// buffer holds the kept bytes.
	buffer bytes.Buffer
}

// Write keeps bytes up to the bound and discards the rest.
//
// Takes data ([]byte) which is the output.
//
// Returns int which is always len(data).
// Returns error which is always nil.
func (bounded *boundedBuffer) Write(data []byte) (int, error) {
	room := max(0, maxReplayOutput-bounded.buffer.Len())
	_, _ = bounded.buffer.Write(data[:min(len(data), room)])
	return len(data), nil
}

// killGroup kills a replay's process group and waits for it.
//
// Takes command (*exec.Cmd) which is the started replay.
// Takes done (<-chan error) which receives its wait result.
func killGroup(command *exec.Cmd, done <-chan error) {
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	<-done
}
