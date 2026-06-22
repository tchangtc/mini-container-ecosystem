package scheduler_test

import (
	"testing"

	"github.com/tcherry/mini-container-ecosystem/mini-kueue/pkg/scheduler"
	"github.com/tcherry/mini-container-ecosystem/mini-kueue/pkg/types"
)

func makeCQ(name string, flavors ...*types.FlavorQuota) *types.ClusterQueue {
	return &types.ClusterQueue{Name: name, Flavors: flavors}
}

func makeFlavor(name types.FlavorName, res types.ResourceList) *types.FlavorQuota {
	return &types.FlavorQuota{Name: name, Resources: res}
}

func setupTest() *scheduler.Scheduler {
	s := scheduler.New()
	cq := makeCQ("test-cq",
		makeFlavor("standard", types.ResourceList{types.ResourceCPU: 4, types.ResourceMemory: 8}),
		makeFlavor("gpu", types.ResourceList{types.ResourceCPU: 2, types.ResourceGPU: 2}),
	)
	s.AddClusterQueue(cq)
	s.AddLocalQueue(&types.LocalQueue{Name: "q", Namespace: "ns", ClusterQueueName: "test-cq"})
	return s
}

func submit(s *scheduler.Scheduler, name string, res types.ResourceList, podCount int, priority int32) {
	w := &types.Workload{Name: name, Namespace: "ns", QueueName: "q", Resources: res, PodCount: podCount, Priority: priority}
	s.Submit(w)
}

func TestFlavorAssignment(t *testing.T) {
	s := setupTest()

	// Submit 2 GPU workloads and 2 CPU workloads
	submit(s, "cpu-1", types.ResourceList{types.ResourceCPU: 2, types.ResourceMemory: 4}, 1, 0)
	submit(s, "cpu-2", types.ResourceList{types.ResourceCPU: 2, types.ResourceMemory: 4}, 1, 0)
	submit(s, "gpu-1", types.ResourceList{types.ResourceCPU: 1, types.ResourceGPU: 1}, 1, 0)
	submit(s, "gpu-2", types.ResourceList{types.ResourceCPU: 1, types.ResourceGPU: 1}, 1, 0)

	admitted := s.Schedule()
	if len(admitted) != 4 {
		t.Fatalf("expected 4 admitted (2 CPU + 2 GPU in separate flavors), got %d", len(admitted))
	}

	// Verify flavors were assigned correctly
	for _, w := range admitted {
		if w.Name == "cpu-1" || w.Name == "cpu-2" {
			if w.FlavorName != "standard" {
				t.Errorf("%s: expected flavor 'standard', got %q", w.Name, w.FlavorName)
			}
		}
		if w.Name == "gpu-1" || w.Name == "gpu-2" {
			if w.FlavorName != "gpu" {
				t.Errorf("%s: expected flavor 'gpu', got %q", w.Name, w.FlavorName)
			}
		}
	}
}

func TestFlavorExhaustion(t *testing.T) {
	s := setupTest()

	// Fill up the GPU flavor (quota=2)
	submit(s, "gpu-1", types.ResourceList{types.ResourceCPU: 1, types.ResourceGPU: 1}, 1, 0)
	submit(s, "gpu-2", types.ResourceList{types.ResourceCPU: 1, types.ResourceGPU: 1}, 1, 0)
	submit(s, "gpu-3", types.ResourceList{types.ResourceCPU: 1, types.ResourceGPU: 1}, 1, 0)

	admitted := s.Schedule()
	if len(admitted) != 2 {
		t.Fatalf("expected 2 GPU admitted (flavor cap 2), got %d", len(admitted))
	}

	cq := s.GetClusterQueue("test-cq")
	if cq == nil {
		t.Fatal("CQ not found")
	}
	if len(cq.Queue) != 1 {
		t.Fatalf("expected 1 pending (gpu-3), got %d", len(cq.Queue))
	}
}

func TestPriorityOrdering(t *testing.T) {
	s := setupTest()

	// Submit in order: low, medium, high
	submit(s, "low", types.ResourceList{types.ResourceCPU: 2, types.ResourceMemory: 4}, 1, 10)
	submit(s, "medium", types.ResourceList{types.ResourceCPU: 2, types.ResourceMemory: 4}, 1, 50)
	submit(s, "high", types.ResourceList{types.ResourceCPU: 2, types.ResourceMemory: 4}, 1, 100)

	// Standard flavor has 4 CPU — only 2 fit
	admitted := s.Schedule()
	if len(admitted) != 2 {
		t.Fatalf("expected 2 admitted, got %d", len(admitted))
	}

	// Higher priority should be admitted first
	if admitted[0].Name != "high" {
		t.Errorf("expected 'high' first, got %q", admitted[0].Name)
	}
	if admitted[1].Name != "medium" {
		t.Errorf("expected 'medium' second, got %q", admitted[1].Name)
	}
}

func TestPriorityFIFOTiebreaker(t *testing.T) {
	s := setupTest()

	// Same priority, different submit times → FIFO within same priority
	// Standard flavor has 4 CPU, GPU flavor has 2 CPU but no memory
	// Submit workloads requiring CPU+memory (only fits standard = 4 CPU total)
	submit(s, "first", types.ResourceList{types.ResourceCPU: 2, types.ResourceMemory: 2}, 1, 50)
	submit(s, "second", types.ResourceList{types.ResourceCPU: 2, types.ResourceMemory: 2}, 1, 50)
	submit(s, "third", types.ResourceList{types.ResourceCPU: 2, types.ResourceMemory: 2}, 1, 50)

	// Only 2 fit (standard flavor: 4 CPU max, GPU flavor doesn't track memory)
	admitted := s.Schedule()
	if len(admitted) != 2 {
		t.Fatalf("expected 2 admitted, got %d", len(admitted))
	}
	if admitted[0].Name != "first" {
		t.Errorf("expected 'first', got %q", admitted[0].Name)
	}
	if admitted[1].Name != "second" {
		t.Errorf("expected 'second', got %q", admitted[1].Name)
	}
}

func TestMultiPodFlavor(t *testing.T) {
	s := scheduler.New()
	cq := makeCQ("multi-cq", makeFlavor("big", types.ResourceList{types.ResourceCPU: 10}))
	s.AddClusterQueue(cq)
	s.AddLocalQueue(&types.LocalQueue{Name: "q", Namespace: "ns", ClusterQueueName: "multi-cq"})

	// 3 pods × 4 CPU = 12 total → won't fit in 10
	w := types.NewWorkload("ml-job", "ns", "q", types.ResourceList{types.ResourceCPU: 4}, 3, 0)
	s.Submit(w)
	admitted := s.Schedule()
	if len(admitted) != 0 {
		t.Fatalf("3x4 CPU should not fit in 10, got %d admitted", len(admitted))
	}
}
