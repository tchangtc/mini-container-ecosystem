package task

import (
	"context"
	"syscall"
	"time"

	tasksv1 "github.com/containerd/containerd/api/services/tasks/v1"
	tasktypes "github.com/containerd/containerd/api/types/task"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service implements containerd's Tasks gRPC service.
type Service struct {
	tasksv1.UnimplementedTasksServer
	mgr *Manager
}

func NewService(mgr *Manager) *Service {
	return &Service{mgr: mgr}
}

func (svc *Service) Create(ctx context.Context, req *tasksv1.CreateTaskRequest) (*tasksv1.CreateTaskResponse, error) {
	// Build minimal spec for the container process
	spec := &specs.Spec{
		Process: &specs.Process{
			Args: []string{req.Stdin},
			Cwd:  "/",
		},
		Root: &specs.Root{Path: "/"},
	}

	bundle := "/tmp/mini-containerd/bundles/" + req.ContainerID

	task, err := svc.mgr.Create(req.ContainerID, bundle, spec)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create task: %v", err)
	}

	return &tasksv1.CreateTaskResponse{
		ContainerID: req.ContainerID,
		Pid:         uint32(task.Pid),
	}, nil
}

func (svc *Service) Start(ctx context.Context, req *tasksv1.StartRequest) (*tasksv1.StartResponse, error) {
	if req.ExecID != "" {
		return nil, status.Errorf(codes.Unimplemented, "exec start not supported")
	}
	if err := svc.mgr.Start(req.ContainerID); err != nil {
		return nil, status.Errorf(codes.Internal, "start task: %v", err)
	}
	return &tasksv1.StartResponse{Pid: 0}, nil
}

func (svc *Service) Delete(ctx context.Context, req *tasksv1.DeleteTaskRequest) (*tasksv1.DeleteResponse, error) {
	if err := svc.mgr.Delete(req.ContainerID); err != nil {
		return nil, status.Errorf(codes.Internal, "delete task: %v", err)
	}
	return &tasksv1.DeleteResponse{}, nil
}

func (svc *Service) DeleteProcess(ctx context.Context, req *tasksv1.DeleteProcessRequest) (*tasksv1.DeleteResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "DeleteProcess not implemented")
}

func (svc *Service) Get(ctx context.Context, req *tasksv1.GetRequest) (*tasksv1.GetResponse, error) {
	t, err := svc.mgr.Get(req.ContainerID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "task %q: %v", req.ContainerID, err)
	}
	return &tasksv1.GetResponse{Process: taskToProto(t)}, nil
}

func (svc *Service) List(ctx context.Context, req *tasksv1.ListTasksRequest) (*tasksv1.ListTasksResponse, error) {
	tasks := svc.mgr.List()
	var result []*tasktypes.Process
	for _, t := range tasks {
		result = append(result, taskToProto(t))
	}
	return &tasksv1.ListTasksResponse{Tasks: result}, nil
}

func (svc *Service) Kill(ctx context.Context, req *tasksv1.KillRequest) (*emptypb.Empty, error) {
	if err := svc.mgr.Kill(req.ContainerID, syscall.Signal(req.Signal)); err != nil {
		return nil, status.Errorf(codes.Internal, "kill task: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (svc *Service) Exec(ctx context.Context, req *tasksv1.ExecProcessRequest) (*emptypb.Empty, error) {
	_ = req.Spec // parsed from Any protobuf in full implementation
	return &emptypb.Empty{}, nil
}

func (svc *Service) Pause(ctx context.Context, req *tasksv1.PauseTaskRequest) (*emptypb.Empty, error) {
	if err := svc.mgr.Pause(req.ContainerID); err != nil {
		return nil, status.Errorf(codes.Internal, "pause: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (svc *Service) Resume(ctx context.Context, req *tasksv1.ResumeTaskRequest) (*emptypb.Empty, error) {
	if err := svc.mgr.Resume(req.ContainerID); err != nil {
		return nil, status.Errorf(codes.Internal, "resume: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (svc *Service) Wait(ctx context.Context, req *tasksv1.WaitRequest) (*tasksv1.WaitResponse, error) {
	exitCode, err := svc.mgr.Wait(req.ContainerID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "wait task: %v", err)
	}
	return &tasksv1.WaitResponse{
		ExitStatus: uint32(exitCode),
		ExitedAt:   timestamppb.Now(),
	}, nil
}

func (svc *Service) ResizePty(ctx context.Context, req *tasksv1.ResizePtyRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (svc *Service) CloseIO(ctx context.Context, req *tasksv1.CloseIORequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (svc *Service) ListPids(ctx context.Context, req *tasksv1.ListPidsRequest) (*tasksv1.ListPidsResponse, error) {
	t, err := svc.mgr.Get(req.ContainerID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "task %q: %v", req.ContainerID, err)
	}
	return &tasksv1.ListPidsResponse{
		Processes: []*tasktypes.ProcessInfo{{Pid: uint32(t.Pid)}},
	}, nil
}

func (svc *Service) Checkpoint(ctx context.Context, req *tasksv1.CheckpointTaskRequest) (*tasksv1.CheckpointTaskResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "checkpoint not implemented")
}

func (svc *Service) Update(ctx context.Context, req *tasksv1.UpdateTaskRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (svc *Service) Metrics(ctx context.Context, req *tasksv1.MetricsRequest) (*tasksv1.MetricsResponse, error) {
	return &tasksv1.MetricsResponse{}, nil
}

// ── Helpers ─────────────────────────────────────────────────────

func taskToProto(t *Task) *tasktypes.Process {
	return &tasktypes.Process{
		ContainerID: t.ID,
		ID:          t.ID,
		Pid:         uint32(t.Pid),
		Status:      taskStatusToProto(t.Status),
		ExitStatus:  uint32(t.ExitCode),
		ExitedAt:    timestamppb.New(time.Now()),
	}
}

func taskStatusToProto(s Status) tasktypes.Status {
	switch s {
	case StatusCreated:
		return tasktypes.Status_CREATED
	case StatusRunning:
		return tasktypes.Status_RUNNING
	case StatusStopped:
		return tasktypes.Status_STOPPED
	case StatusPaused:
		return tasktypes.Status_PAUSED
	default:
		return tasktypes.Status_UNKNOWN
	}
}
