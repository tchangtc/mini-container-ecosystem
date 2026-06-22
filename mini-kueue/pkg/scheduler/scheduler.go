// Package scheduler implements a priority-aware, flavor-based workload
// scheduler for mini-kueue. It supports:
//   - Priority ordering (higher priority first, FIFO within same priority)
//   - Flavor-aware admission (match workload to available resource flavor)
//   - Multi-ClusterQueue routing via LocalQueue
package scheduler

import (
	"fmt"
	"sort"

	"github.com/tcherry/mini-container-ecosystem/mini-kueue/pkg/types"
)

// Scheduler manages workload admission across ClusterQueues.
type Scheduler struct {
	clusterQueues map[string]*types.ClusterQueue
	localQueues   map[string]*types.LocalQueue
}

// New creates a new scheduler.
func New() *Scheduler {
	return &Scheduler{
		clusterQueues: make(map[string]*types.ClusterQueue),
		localQueues:   make(map[string]*types.LocalQueue),
	}
}

// AddClusterQueue registers a ClusterQueue.
func (s *Scheduler) AddClusterQueue(cq *types.ClusterQueue) {
	s.clusterQueues[cq.Name] = cq
}

// AddLocalQueue registers a LocalQueue.
func (s *Scheduler) AddLocalQueue(lq *types.LocalQueue) {
	s.localQueues[lq.Namespace+"/"+lq.Name] = lq
}

// Submit enqueues a workload into its LocalQueue and routes to the
// associated ClusterQueue.
func (s *Scheduler) Submit(w *types.Workload) error {
	key := w.Namespace + "/" + w.QueueName
	lq, ok := s.localQueues[key]
	if !ok {
		return fmt.Errorf("localqueue %q not found", key)
	}
	cq, ok := s.clusterQueues[lq.ClusterQueueName]
	if !ok {
		return fmt.Errorf("clusterqueue %q not found", lq.ClusterQueueName)
	}
	cq.Enqueue(w)
	lq.Workloads = append(lq.Workloads, w)
	return nil
}

// Schedule runs one scheduling pass over all ClusterQueues.
// For each, it dequeues workloads by priority and tries to assign
// a flavor that can fit. If no flavor fits, the workload remains queued.
func (s *Scheduler) Schedule() []*types.Workload {
	var admitted []*types.Workload

	for _, cq := range s.clusterQueues {
		// Collect all workloads from this CQ and sort by priority
		var toRetry []*types.Workload
		for {
			w := cq.Dequeue()
			if w == nil {
				break
			}
			if _, err := cq.AssignFlavor(w); err != nil {
				toRetry = append(toRetry, w)
			} else {
				admitted = append(admitted, w)
			}
		}
		// Re-enqueue workloads that couldn't be admitted
		for _, w := range toRetry {
			cq.Enqueue(w)
		}
	}
	return admitted
}

// FinishByName finds and completes a workload by name/namespace/queue.
func (s *Scheduler) FinishByName(namespace, queueName, workloadName string) error {
	lq, ok := s.localQueues[namespace+"/"+queueName]
	if !ok {
		return fmt.Errorf("localqueue %q not found", namespace+"/"+queueName)
	}
	cq, ok := s.clusterQueues[lq.ClusterQueueName]
	if !ok {
		return fmt.Errorf("clusterqueue %q not found", lq.ClusterQueueName)
	}
	for _, w := range cq.Workloads {
		if w.Name == workloadName && w.Namespace == namespace {
			cq.ReleaseWorkload(w)
			return nil
		}
	}
	return fmt.Errorf("workload %q not found in %q", workloadName, cq.Name)
}

// Finish releases resources for a workload.
func (s *Scheduler) Finish(w *types.Workload) error {
	lq, ok := s.localQueues[w.Namespace+"/"+w.QueueName]
	if !ok {
		return fmt.Errorf("localqueue %q not found", w.Namespace+"/"+w.QueueName)
	}
	cq, ok := s.clusterQueues[lq.ClusterQueueName]
	if !ok {
		return fmt.Errorf("clusterqueue %q not found", lq.ClusterQueueName)
	}
	cq.ReleaseWorkload(w)
	return nil
}

// GetClusterQueue returns a ClusterQueue by name.
func (s *Scheduler) GetClusterQueue(name string) *types.ClusterQueue {
	return s.clusterQueues[name]
}

// Status returns a formatted string of the current queue state.
func (s *Scheduler) Status() string {
	var result string
	names := make([]string, 0, len(s.clusterQueues))
	for name := range s.clusterQueues {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		result += s.clusterQueues[name].Status() + "\n"
	}
	return result
}
