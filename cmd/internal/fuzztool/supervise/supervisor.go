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

package supervise

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"seagull.sh/seagull/cmd/internal/fuzztool/fuzztext"
)

const (
	// StatusFailClosed is the exit status for a stopped command or a monitor failure.
	StatusFailClosed = 125

	// signalStatusBase is added to a signal number for a command a signal ended.
	signalStatusBase = 128

	// OutcomeInterrupted records a command stopped by an external interrupt.
	OutcomeInterrupted = "interrupted"

	// OutcomeResourceStopped records a command stopped at a resource limit.
	OutcomeResourceStopped = "resource-stopped"

	// OutcomeCleanupFailed records a command that left processes behind.
	OutcomeCleanupFailed = "cleanup-failed"

	// OutcomeMonitorFailed records a failure of the supervisor itself.
	OutcomeMonitorFailed = "monitor-failed"

	// tokenVariable names the environment variable that carries the command token.
	tokenVariable = "SEAGULL_FUZZ_COMMAND_TOKEN" //nolint:gosec // A variable name, not a credential.

	// maxLeaseBytes bounds the compiled-cache lease record.
	maxLeaseBytes = 4096

	// maxProcessFileBytes bounds a process identifier file.
	maxProcessFileBytes = 64

	// tokenBytes is the number of random bytes in a command token.
	tokenBytes = 16

	// fileMode is the permission of every metadata file written.
	fileMode = 0o600

	// pollInterval is how often the supervisor wakes while the command runs.
	pollInterval = 100 * time.Millisecond

	// resourceInterval is how often resources are checked.
	resourceInterval = 5 * time.Second

	// terminateDelay is how long after a stop the group receives SIGTERM.
	terminateDelay = 30 * time.Second

	// killDelay is how long after a stop the group receives SIGKILL.
	killDelay = 35 * time.Second

	// cleanupGrace is how long leftover processes have to exit after SIGTERM.
	cleanupGrace = 5 * time.Second
)

var (
	// errUnsafeLease refuses lease metadata that is a link or oversized.
	errUnsafeLease = errors.New("unsafe compiled-cache lease metadata")

	// errInvalidLease refuses a lease record that is not a JSON object.
	errInvalidLease = errors.New("invalid compiled-cache lease record")

	// errInvalidWorker refuses a lease record without a positive process identifier.
	errInvalidWorker = errors.New("invalid compiled-cache worker identity")

	// errWorkerSurvived reports a leased worker still alive after cleanup.
	errWorkerSurvived = errors.New("compiled-cache worker survived command cleanup")

	// errNoCommand refuses an empty command line.
	errNoCommand = errors.New("no command to run")
)

// Config holds the limits and locations one supervised command runs under.
type Config struct {
	// ManagedRoot is the campaign's managed storage directory.
	ManagedRoot string

	// MetadataDirectory receives the outcome, launcher and execution records.
	MetadataDirectory string

	// Locations are further paths whose filesystems must keep MinimumFreeBytes free.
	Locations []string

	// MinimumFreeBytes is the free-space floor on every affected filesystem.
	MinimumFreeBytes uint64

	// MaximumManagedBytes stops the command once managed storage reaches it.
	MaximumManagedBytes uint64
}

// timings holds the supervisor's intervals, shortened by tests.
type timings struct {
	// poll is how often the supervisor wakes while the command runs.
	poll time.Duration

	// resourceCheck is how often resources are checked.
	resourceCheck time.Duration

	// terminateAfter is how long after a stop the group receives SIGTERM.
	terminateAfter time.Duration

	// killAfter is how long after a stop the group receives SIGKILL.
	killAfter time.Duration

	// cleanupGrace is how long leftover processes have to exit after SIGTERM.
	cleanupGrace time.Duration
}

// Supervisor runs one command in a fresh process group and records how it ended.
type Supervisor struct {
	// stdout receives resource notices.
	stdout io.Writer

	// signals delivers external interrupts.
	signals <-chan os.Signal

	// command is the running command, nil until it starts.
	command *exec.Cmd

	// done receives the command's wait result.
	done chan error

	// started is when the supervisor began.
	started time.Time

	// stoppedAt is when a stop was requested.
	stoppedAt time.Time

	// reason is the recorded terminal override, empty while there is none.
	reason string

	// token identifies this command to the compiled-cache lease.
	token string

	// arguments is the command line to run.
	arguments []string

	// config holds the limits and locations.
	config Config

	// timings holds the intervals.
	timings timings

	// peakManaged is the largest managed storage size seen.
	peakManaged uint64

	// escalation counts the escalating signals already sent after a stop.
	escalation int

	// stopped is true once a stop was requested.
	stopped bool

	// waited is true once the command has been reaped.
	waited bool
}

// executionRecord is the execution.json written after every started command.
type executionRecord struct {
	// Outcome is the recorded override, or passed or failed.
	Outcome string `json:"outcome"`

	// ChildMaxRSSUnit is the unit of ChildMaxRSS on this platform.
	ChildMaxRSSUnit string `json:"child_max_rss_unit"`

	// Version is the record format version.
	Version int `json:"version"`

	// ExitCode is the command's status, negative for a signal.
	ExitCode int `json:"exit_code"`

	// ElapsedSeconds is the supervisor's wall time.
	ElapsedSeconds float64 `json:"elapsed_seconds"`

	// PeakManagedBytes is the largest managed storage size seen.
	PeakManagedBytes uint64 `json:"peak_managed_bytes"`

	// ChildCPUSeconds is the user and system time of reaped children.
	ChildCPUSeconds float64 `json:"child_cpu_seconds"`

	// ChildMaxRSS is the largest resident set of a reaped child.
	ChildMaxRSS int64 `json:"child_max_rss"`

	// ProcessGroup is the command's process group.
	ProcessGroup int `json:"process_group"`
}

// New prepares a supervisor for one command.
//
// Takes config (Config) which holds the limits and locations.
// Takes arguments ([]string) which is the command line to run.
// Takes signals (<-chan os.Signal) which delivers external interrupts.
// Takes stdout (io.Writer) which receives resource notices.
//
// Returns *Supervisor which is ready to run.
func New(config Config, arguments []string, signals <-chan os.Signal, stdout io.Writer) *Supervisor {
	return &Supervisor{
		started:   time.Now(),
		stoppedAt: time.Time{},
		stdout:    stdout,
		signals:   signals,
		command:   nil,
		done:      make(chan error, 1),
		reason:    "",
		token:     "",
		config:    config,
		arguments: arguments,
		timings: timings{
			poll:           pollInterval,
			resourceCheck:  resourceInterval,
			terminateAfter: terminateDelay,
			killAfter:      killDelay,
			cleanupGrace:   cleanupGrace,
		},
		peakManaged: 0,
		escalation:  0,
		stopped:     false,
		waited:      false,
	}
}

// Run starts the command, supervises it to the end and cleans up its group.
//
// A monitor failure records monitor-failed, kills the group and fails closed.
//
// Takes stderr (io.Writer) which receives monitor failures.
//
// Returns int which is the command's status, 128 plus a signal number for a command a
// signal ended, or StatusFailClosed for a stop or a monitor failure.
func (supervisor *Supervisor) Run(stderr io.Writer) int {
	status, err := supervisor.run()
	if err == nil {
		return status
	}
	fmt.Fprintf(stderr, "Fuzz command monitor failed: %v\n", err)
	_ = supervisor.outcome(OutcomeMonitorFailed)
	if supervisor.command == nil {
		return StatusFailClosed
	}
	_ = supervisor.signalGroup(syscall.SIGKILL)
	if !supervisor.waited {
		<-supervisor.done
		supervisor.waited = true
	}
	if err := supervisor.releaseCache(); err != nil {
		fmt.Fprintf(stderr, "Cache lease cleanup failed: %v\n", err)
	}
	_ = supervisor.record(StatusFailClosed)
	return StatusFailClosed
}

// run checks resources, starts the command and supervises it.
//
// Returns int which is the exit status.
// Returns error when the supervisor itself fails.
func (supervisor *Supervisor) run() (int, error) {
	if len(supervisor.arguments) == 0 {
		return 0, errNoCommand
	}
	healthy, err := supervisor.checkResources()
	if err != nil {
		return 0, err
	}
	if !healthy {
		return StatusFailClosed, supervisor.outcome(OutcomeResourceStopped)
	}
	if err := supervisor.start(); err != nil {
		return 0, err
	}
	result, err := supervisor.monitor()
	if err != nil {
		return 0, err
	}
	if err := supervisor.cleanGroup(); err != nil {
		return 0, err
	}
	supervisor.drainSignals()
	if err := supervisor.releaseCache(); err != nil {
		return 0, err
	}
	if err := supervisor.record(result); err != nil {
		return 0, err
	}
	if supervisor.reason != "" {
		return StatusFailClosed, nil
	}
	if result < 0 {
		return signalStatusBase - result, nil
	}
	return result, nil
}

// start launches the command in a new session and records the launcher.
//
// Returns error when token generation, command startup or launcher recording fails.
//
// Concurrency: start reaps the command in a goroutine and sends the result to
// supervisor.done.
func (supervisor *Supervisor) start() error {
	random := make([]byte, tokenBytes)
	if _, err := rand.Read(random); err != nil {
		return fmt.Errorf("creating the command token: %w", err)
	}
	supervisor.token = hex.EncodeToString(random)
	command := exec.Command(supervisor.arguments[0], supervisor.arguments[1:]...) //nolint:gosec // Runs the command the campaign chose.
	command.Env = append(os.Environ(), tokenVariable+"="+supervisor.token)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", supervisor.arguments[0], err)
	}
	supervisor.command = command
	go func() { supervisor.done <- command.Wait() }()
	launcher := strconv.Itoa(os.Getpid()) + "\n"
	path := filepath.Join(supervisor.config.MetadataDirectory, "launcher.pid")
	if err := os.WriteFile(path, []byte(launcher), fileMode); err != nil {
		return fmt.Errorf("recording the launcher: %w", err)
	}
	return nil
}

// monitor waits for the command, forwarding interrupts, checking resources and
// escalating a stop the command ignores.
//
// Returns int which is the command's status, negative for a signal.
// Returns error when a check or a signal fails.
func (supervisor *Supervisor) monitor() (int, error) {
	ticker := time.NewTicker(supervisor.timings.poll)
	defer ticker.Stop()
	checked := time.Now()
	for {
		select {
		case <-supervisor.signals:
			if err := supervisor.stop(OutcomeInterrupted); err != nil {
				return 0, err
			}
		case <-supervisor.done:
			supervisor.waited = true
			return exitResult(supervisor.command.ProcessState), nil
		case <-ticker.C:
			if err := supervisor.tick(&checked); err != nil {
				return 0, err
			}
		}
	}
}

// tick escalates a stop the command ignores, or checks resources when they are due.
//
// Takes checked (*time.Time) which is when resources were last checked.
//
// Returns error when a check or a signal fails.
func (supervisor *Supervisor) tick(checked *time.Time) error {
	if supervisor.stopped {
		return supervisor.escalate()
	}
	if time.Since(*checked) < supervisor.timings.resourceCheck {
		return nil
	}
	*checked = time.Now()
	return supervisor.stopAtLimits()
}

// stopAtLimits stops the command when a resource limit is reached.
//
// Returns error when resources cannot be read or the stop fails.
func (supervisor *Supervisor) stopAtLimits() error {
	healthy, err := supervisor.checkResources()
	if err != nil || healthy {
		return err
	}
	return supervisor.stop(OutcomeResourceStopped)
}

// drainSignals records an interrupt that arrived while the group was cleaned up.
func (supervisor *Supervisor) drainSignals() {
	for {
		select {
		case <-supervisor.signals:
			_ = supervisor.stop(OutcomeInterrupted)
		default:
			return
		}
	}
}

// outcome persists a terminal override before the command is interrupted.
//
// Takes reason (string) which is the override.
//
// Returns error when the record cannot be written.
func (supervisor *Supervisor) outcome(reason string) error {
	supervisor.reason = reason
	temporary := filepath.Join(supervisor.config.MetadataDirectory, "outcome.tmp")
	if err := os.WriteFile(temporary, []byte(reason+"\n"), fileMode); err != nil {
		return fmt.Errorf("recording the outcome: %w", err)
	}
	if err := os.Rename(temporary, filepath.Join(supervisor.config.MetadataDirectory, "outcome.txt")); err != nil {
		return fmt.Errorf("recording the outcome: %w", err)
	}
	return nil
}

// stop records a reason and interrupts the fuzz coordinator, or the command itself.
//
// Takes reason (string) which is the override to record.
//
// Returns error when the outcome cannot be recorded or the interrupt fails.
func (supervisor *Supervisor) stop(reason string) error {
	if supervisor.stopped {
		return nil
	}
	if err := supervisor.outcome(reason); err != nil {
		return err
	}
	supervisor.stopped = true
	supervisor.stoppedAt = time.Now()
	if supervisor.command == nil || supervisor.waited {
		return nil
	}
	target := supervisor.command.Process.Pid
	if coordinator, found := supervisor.coordinator(); found {
		target = coordinator
	}
	err := syscall.Kill(target, syscall.SIGINT)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("interrupting process %d: %w", target, err)
	}
	return nil
}

// coordinator reads the fuzz coordinator's process when it belongs to this group.
//
// Returns int which is the coordinator's process identifier.
// Returns bool which is true when the record names a process in the group.
func (supervisor *Supervisor) coordinator() (int, bool) {
	content, err := readBounded(filepath.Join(supervisor.config.MetadataDirectory, "coordinator.pid"), maxProcessFileBytes)
	if err != nil {
		return 0, false
	}
	candidate, err := strconv.Atoi(string(bytes.TrimSpace(content)))
	if err != nil || candidate <= 0 {
		return 0, false
	}
	group, err := syscall.Getpgid(candidate)
	if err != nil || group != supervisor.command.Process.Pid {
		return 0, false
	}
	return candidate, true
}

// escalate signals the group when an interrupted command ignores cancellation.
//
// Returns error when the group cannot be signalled.
func (supervisor *Supervisor) escalate() error {
	elapsed := time.Since(supervisor.stoppedAt)
	if elapsed >= supervisor.timings.killAfter && supervisor.escalation < 2 {
		supervisor.escalation = 2
		return supervisor.signalGroup(syscall.SIGKILL)
	}
	if elapsed >= supervisor.timings.terminateAfter && supervisor.escalation < 1 {
		supervisor.escalation = 1
		return supervisor.signalGroup(syscall.SIGTERM)
	}
	return nil
}

// signalGroup signals only the process group this command created.
//
// Takes signal (syscall.Signal) which is the signal to send.
//
// Returns error when the signal fails for a reason other than an empty group.
func (supervisor *Supervisor) signalGroup(signal syscall.Signal) error {
	err := syscall.Kill(-supervisor.command.Process.Pid, signal)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signalling the command group: %w", err)
	}
	return nil
}

// groupAlive reports whether any process remains in the command's group.
//
// Returns bool which is true while the group has a member.
// Returns error when the group cannot be probed.
func (supervisor *Supervisor) groupAlive() (bool, error) {
	err := syscall.Kill(-supervisor.command.Process.Pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("probing the command group: %w", err)
	}
	return true, nil
}

// cleanGroup ends descendants the command left behind, recording cleanup-failed.
//
// Returns error when the group cannot be probed or signalled.
func (supervisor *Supervisor) cleanGroup() error {
	alive, err := supervisor.groupAlive()
	if err != nil || !alive {
		return err
	}
	if supervisor.reason == "" {
		if err := supervisor.outcome(OutcomeCleanupFailed); err != nil {
			return err
		}
	}
	if err := supervisor.signalGroup(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(supervisor.timings.cleanupGrace)
	for time.Now().Before(deadline) {
		alive, err := supervisor.groupAlive()
		if err != nil || !alive {
			return err
		}
		time.Sleep(supervisor.timings.poll)
	}
	return supervisor.signalGroup(syscall.SIGKILL)
}

// checkResources checks every affected filesystem and the managed storage threshold.
//
// Returns bool which is false once a limit is reached.
// Returns error when free space or managed storage cannot be read.
func (supervisor *Supervisor) checkResources() (bool, error) {
	locations := append([]string{supervisor.config.ManagedRoot}, supervisor.config.Locations...)
	checked := make(map[string]bool, len(locations))
	for _, location := range locations {
		location = existingAncestor(location)
		if checked[location] {
			continue
		}
		checked[location] = true
		free, err := freeBytes(location)
		if err != nil {
			return false, err
		}
		if free < supervisor.config.MinimumFreeBytes {
			fmt.Fprintf(supervisor.stdout, "Fuzz command stopped at the free-space floor for %s.\n", location)
			return false, nil
		}
	}
	used, err := AllocatedBytes(supervisor.config.ManagedRoot)
	if err != nil {
		return false, err
	}
	supervisor.peakManaged = max(supervisor.peakManaged, used)
	if used >= supervisor.config.MaximumManagedBytes {
		fmt.Fprintln(supervisor.stdout, "Fuzz command reached the managed storage stop threshold.")
		return false, nil
	}
	return true, nil
}

// releaseCache releases a dead worker's compiled-cache lease that bears this
// command's token, leaving every other lease alone.
//
// Returns error when the lease is unsafe, malformed or held by a live worker.
func (supervisor *Supervisor) releaseCache() error {
	lease := filepath.Join(supervisor.config.ManagedRoot, "programs", "lease")
	owner := filepath.Join(lease, "owner.json")
	if _, err := os.Stat(owner); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	process, err := supervisor.leaseWorker(lease, owner)
	if err != nil || process == 0 {
		return err
	}
	err = syscall.Kill(process, 0)
	if err == nil {
		return errWorkerSurvived
	}
	if !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("probing the cache worker: %w", err)
	}
	if err := os.Remove(owner); err != nil {
		return fmt.Errorf("releasing the cache lease: %w", err)
	}
	if err := os.Remove(lease); err != nil {
		return fmt.Errorf("releasing the cache lease: %w", err)
	}
	return nil
}

// leaseWorker reads the worker that holds a lease bearing this command's token.
//
// Takes lease (string) which is the lease directory.
// Takes owner (string) which is the lease record.
//
// Returns int which is the worker's process identifier, zero for another command's lease.
// Returns error when the lease is unsafe or malformed.
func (supervisor *Supervisor) leaseWorker(lease string, owner string) (int, error) {
	for _, path := range []string{lease, owner} {
		info, err := os.Lstat(path)
		if err != nil {
			return 0, fmt.Errorf("reading the cache lease: %w", err)
		}
		if info.Mode()&fs.ModeSymlink != 0 || path == owner && info.Size() > maxLeaseBytes {
			return 0, errUnsafeLease
		}
	}
	content, err := readBounded(owner, maxLeaseBytes)
	if err != nil {
		return 0, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var record map[string]any
	if err := decoder.Decode(&record); err != nil || record == nil {
		return 0, errInvalidLease
	}
	if token, isString := record["CommandToken"].(string); !isString || token != supervisor.token {
		return 0, nil
	}
	number, isNumber := record["PID"].(json.Number)
	if !isNumber {
		return 0, errInvalidWorker
	}
	process, err := strconv.Atoi(number.String())
	if err != nil || process <= 0 {
		return 0, errInvalidWorker
	}
	return process, nil
}

// record writes resource measurements, never connection strings or secrets.
//
// Takes result (int) which is the command's status.
//
// Returns error when the record cannot be written.
func (supervisor *Supervisor) record(result int) error {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &usage); err != nil {
		return fmt.Errorf("reading child resource usage: %w", err)
	}
	outcome := supervisor.reason
	if outcome == "" {
		outcome = "failed"
		if result == 0 {
			outcome = "passed"
		}
	}
	unit := "KiB"
	if runtime.GOOS == "darwin" {
		unit = "bytes"
	}
	encoded, err := fuzztext.MarshalASCII(executionRecord{
		Version:          1,
		Outcome:          outcome,
		ExitCode:         result,
		ElapsedSeconds:   time.Since(supervisor.started).Seconds(),
		PeakManagedBytes: supervisor.peakManaged,
		ChildCPUSeconds:  time.Duration(usage.Utime.Nano() + usage.Stime.Nano()).Seconds(),
		ChildMaxRSS:      int64(usage.Maxrss),
		ChildMaxRSSUnit:  unit,
		ProcessGroup:     supervisor.command.Process.Pid,
	}, true)
	if err != nil {
		return err
	}
	path := filepath.Join(supervisor.config.MetadataDirectory, "execution.json")
	if err := os.WriteFile(path, append(encoded, '\n'), fileMode); err != nil {
		return fmt.Errorf("recording the execution: %w", err)
	}
	return nil
}

// exitResult turns a wait status into a shell-style result.
//
// Takes state (*os.ProcessState) which is the reaped command's state.
//
// Returns int which is the exit status, or the negated signal for a signalled command.
func exitResult(state *os.ProcessState) int {
	status, isWaitStatus := state.Sys().(syscall.WaitStatus)
	if isWaitStatus && status.Signaled() {
		return -int(status.Signal())
	}
	return state.ExitCode()
}

// existingAncestor returns the nearest existing path at or above location.
//
// Takes location (string) which is the path to resolve.
//
// Returns string which exists, or the filesystem root.
func existingAncestor(location string) string {
	for {
		if _, err := os.Stat(location); err == nil {
			return location
		}
		parent := filepath.Dir(location)
		if parent == location {
			return location
		}
		location = parent
	}
}

// readBounded reads at most limit bytes of a file, refusing a longer one.
//
// Takes path (string) which is the file to read.
// Takes limit (int64) which is the largest accepted size.
//
// Returns []byte which holds the content.
// Returns error when the file cannot be read or exceeds the limit.
func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path) //nolint:gosec // Paths come from the campaign's own metadata.
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("reading %s: %w", path, errUnsafeLease)
	}
	return content, nil
}
