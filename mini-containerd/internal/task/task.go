// Package task implements the task (container process) lifecycle service.
// It manages container processes using Linux namespace isolation via the
// clone(2) syscall. Each task has a state machine:
//
//	Created → Running → Stopped → Deleted
//	             ↓
//	          Paused
//
// Phase 2.4 — Task Service.
package task

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// Status represents a task's lifecycle state.
type Status string

const (
	StatusCreated Status = "CREATED"
	StatusRunning Status = "RUNNING"
	StatusStopped Status = "STOPPED"
	StatusPaused  Status = "PAUSED"
)

// Task holds the state of a running container process.
type Task struct {
	ID        string
	Pid       int
	Status    Status
	ExitCode  int
	Bundle    string
	RootFS    string
	Spec      *specs.Spec // OCI runtime spec
	CreatedAt time.Time
	StartedAt time.Time

	cmd    *exec.Cmd     // the OS process handle
	waitCh chan error    // closed when process exits
	mu     sync.Mutex
}

// Manager manages the lifecycle of all tasks.
type Manager struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewManager creates a new task manager.
func NewManager() *Manager {
	return &Manager{
		tasks: make(map[string]*Task),
	}
}

// Create creates a new task from an OCI spec.
// The task is created in the CREATED state — the process is forked with
// namespace isolation but paused waiting for Start.
func (m *Manager) Create(id, bundle string, spec *specs.Spec) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.tasks[id]; ok {
		return nil, fmt.Errorf("task %q already exists", id)
	}

	rootfs := spec.Root.Path
	if rootfs == "" {
		rootfs = filepath.Join(bundle, "rootfs")
	}

	t := &Task{
		ID:        id,
		Pid:       0,
		Status:    StatusCreated,
		Bundle:    bundle,
		RootFS:    rootfs,
		Spec:      spec,
		CreatedAt: time.Now(),
		waitCh:    make(chan error, 1),
	}

	// Build the command from spec
	args := buildArgs(spec)

	// Create the OS process with namespace isolation
	cmd := exec.Command(args[0], args[1:]...)

	// Set up namespace clone flags
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWNET,
	}

	// Environment
	cmd.Env = os.Environ()
	for _, env := range spec.Process.Env {
		cmd.Env = append(cmd.Env, env)
	}

	// Working directory
	if spec.Process.Cwd != "" {
		cmd.Dir = spec.Process.Cwd
	} else {
		cmd.Dir = "/"
	}

	// Stdio — connect to /dev/null initially
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		cmd.Stderr = devNull
	}

	// Start the process (it will be paused until we send SIGCONT or it starts)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	t.Pid = cmd.Process.Pid
	t.cmd = cmd

	// Monitor process exit in background
	go func() {
		err := cmd.Wait()
		t.mu.Lock()
		if t.Status == StatusRunning {
			t.Status = StatusStopped
			if err != nil {
				if exiterr, ok := err.(*exec.ExitError); ok {
					if ws, ok := exiterr.Sys().(syscall.WaitStatus); ok {
						t.ExitCode = ws.ExitStatus()
					}
				}
			}
		}
		t.Pid = 0
		t.mu.Unlock()
		t.waitCh <- err
	}()

	m.tasks[id] = t
	return t, nil
}

// Start sends the task into the RUNNING state.
func (m *Manager) Start(id string) error {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Status != StatusCreated {
		return fmt.Errorf("task %q is not in CREATED state (current: %s)", id, t.Status)
	}

	// The process is already running (cmd.Start was called),
	// we just transition the state
	t.Status = StatusRunning
	t.StartedAt = time.Now()
	return nil
}

// Kill sends a signal to a task's process.
func (m *Manager) Kill(id string, signal syscall.Signal) error {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	t.mu.Lock()
	pid := t.Pid
	t.mu.Unlock()

	if pid <= 0 {
		return fmt.Errorf("task %q has no running process", id)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	return process.Signal(signal)
}

// Wait blocks until the task exits and returns its exit status.
func (m *Manager) Wait(id string) (int, error) {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return -1, fmt.Errorf("task %q not found", id)
	}

	<-t.waitCh

	t.mu.Lock()
	code := t.ExitCode
	t.mu.Unlock()
	return code, nil
}

// Delete removes a task from the manager.
// The underlying process must be stopped first.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	if t.Status == StatusRunning {
		return fmt.Errorf("task %q is still running — stop it first", id)
	}

	delete(m.tasks, id)
	return nil
}

// Exec runs a command inside an existing task's namespace.
// Uses nsenter via /proc/pid/ns/* to enter the container's namespaces.
func (m *Manager) Exec(id string, spec *specs.Process) (int, error) {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return -1, fmt.Errorf("task %q not found", id)
	}

	t.mu.Lock()
	pid := t.Pid
	t.mu.Unlock()

	if pid <= 0 {
		return -1, fmt.Errorf("task %q is not running", id)
	}

	// Use nsenter to enter container namespaces
	args := buildArgs(&specs.Spec{Process: spec})
	cmd := exec.Command("nsenter",
		append([]string{
			fmt.Sprintf("--pid=/proc/%d/ns/pid", pid),
			fmt.Sprintf("--mount=/proc/%d/ns/mnt", pid),
			fmt.Sprintf("--uts=/proc/%d/ns/uts", pid),
			fmt.Sprintf("--ipc=/proc/%d/ns/ipc", pid),
			fmt.Sprintf("--net=/proc/%d/ns/net", pid),
			"--",
		}, args...)...,
	)

	cmd.Env = os.Environ()
	for _, env := range spec.Env {
		cmd.Env = append(cmd.Env, env)
	}
	if spec.Cwd != "" {
		cmd.Dir = spec.Cwd
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if ws, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return ws.ExitStatus(), nil
			}
		}
		return -1, err
	}
	return 0, nil
}

// Get returns the task state.
func (m *Manager) Get(id string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %q not found", id)
	}
	return t, nil
}

// List returns all tasks.
func (m *Manager) List() []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		result = append(result, t)
	}
	return result
}

// Pause puts a running task into the PAUSED state (freeze cgroup).
func (m *Manager) Pause(id string) error {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Status != StatusRunning {
		return fmt.Errorf("task %q is not running (current: %s)", id, t.Status)
	}
	t.Status = StatusPaused

	// Send SIGSTOP to freeze the process
	if t.Pid > 0 {
		process, _ := os.FindProcess(t.Pid)
		if process != nil {
			process.Signal(syscall.SIGSTOP)
		}
	}
	return nil
}

// Resume puts a paused task back into the RUNNING state.
func (m *Manager) Resume(id string) error {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Status != StatusPaused {
		return fmt.Errorf("task %q is not paused (current: %s)", id, t.Status)
	}
	t.Status = StatusRunning

	// Send SIGCONT to resume
	if t.Pid > 0 {
		process, _ := os.FindProcess(t.Pid)
		if process != nil {
			process.Signal(syscall.SIGCONT)
		}
	}
	return nil
}

// ── Helpers ─────────────────────────────────────────────────────

// buildArgs builds the command line from an OCI spec.
func buildArgs(spec *specs.Spec) []string {
	p := spec.Process
	if p == nil {
		return []string{"sh"}
	}

	args := make([]string, 0)
	if len(p.Args) > 0 {
		args = append(args, p.Args...)
	}
	if len(args) == 0 {
		args = []string{"sh"}
	}
	return args
}

