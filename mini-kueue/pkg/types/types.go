// Package types defines the core data structures for mini-kueue.
// These mirror Kueue's CRDs but are standalone Go types with no K8s dependency.
package types

import (
	"fmt"
	"sort"
	"time"
)

// ── Workload ────────────────────────────────────────────────────

type WorkloadStatus string

const (
	StatusPending  WorkloadStatus = "Pending"
	StatusAdmitted WorkloadStatus = "Admitted"
	StatusRejected WorkloadStatus = "Rejected"
	StatusFinished WorkloadStatus = "Finished"
)

// NewWorkload creates a workload with CreatedAt set to now.
func NewWorkload(name, namespace, queueName string, resources ResourceList, podCount int, priority int32) *Workload {
	return &Workload{
		Name: name, Namespace: namespace, QueueName: queueName,
		Resources: resources, PodCount: podCount, Priority: priority,
		CreatedAt: time.Now(),
	}
}

// Workload represents a job requesting resources.
type Workload struct {
	Name       string
	Namespace  string
	QueueName  string
	Resources  ResourceList
	FlavorName FlavorName // assigned flavor after admission
	PodCount   int
	Priority   int32
	Status     WorkloadStatus
	CreatedAt  time.Time
	AdmittedAt *time.Time
	FinishedAt *time.Time
}

// TotalRequests returns total resources (per-pod * count).
func (w *Workload) TotalRequests() ResourceList {
	result := make(ResourceList)
	for k, v := range w.Resources {
		result[k] = v * float64(w.PodCount)
	}
	return result
}

// ── Resource Types ──────────────────────────────────────────────

type ResourceName string

const (
	ResourceCPU    ResourceName = "cpu"
	ResourceMemory ResourceName = "memory"
	ResourceGPU    ResourceName = "nvidia.com/gpu"
)

type ResourceList map[ResourceName]float64

func (r ResourceList) Add(other ResourceList) ResourceList {
	result := make(ResourceList)
	for k, v := range r {
		result[k] = v
	}
	for k, v := range other {
		result[k] += v
	}
	return result
}

func (r ResourceList) Sub(other ResourceList) ResourceList {
	result := make(ResourceList)
	for k, v := range r {
		result[k] = v
	}
	for k, v := range other {
		result[k] -= v
	}
	return result
}

func (r ResourceList) Fits(capacity ResourceList) bool {
	for k, v := range r {
		if cap, ok := capacity[k]; ok && v > cap {
			return false
		}
	}
	return true
}

func (r ResourceList) Clone() ResourceList {
	result := make(ResourceList)
	for k, v := range r {
		result[k] = v
	}
	return result
}

// ── Flavor ──────────────────────────────────────────────────────

type FlavorName string

// FlavorQuota represents a named resource bucket within a ClusterQueue.
// Each flavor has its own quota and usage tracking.
type FlavorQuota struct {
	Name      FlavorName
	Resources ResourceList // allocatable resources per flavor
	Used      ResourceList // currently allocated
}

// Available returns free resources in this flavor.
func (fq *FlavorQuota) Available() ResourceList {
	return fq.Resources.Clone().Sub(fq.Used)
}

// CanFit returns true if the flavor tracks all requested resources AND
// has enough available capacity.
func (fq *FlavorQuota) CanFit(req ResourceList) bool {
	// Flavor must support ALL requested resources
	for k := range req {
		if _, ok := fq.Resources[k]; !ok {
			return false // flavor doesn't support this resource type
		}
	}
	return req.Fits(fq.Available())
}

// Reserve allocates resources from this flavor.
func (fq *FlavorQuota) Reserve(req ResourceList) {
	fq.Used = fq.Used.Add(req)
}

// Release frees resources from this flavor.
func (fq *FlavorQuota) Release(req ResourceList) {
	fq.Used = fq.Used.Sub(req)
}

// ── Queue Types ─────────────────────────────────────────────────

// ClusterQueue is a cluster-scoped resource pool with per-flavor quotas.
type ClusterQueue struct {
	Name      string
	Flavors   []*FlavorQuota // per-flavor resource quotas
	Workloads []*Workload    // admitted workloads
	Queue     []*Workload    // pending workloads (sorted by priority)
}

// AssignFlavor finds a flavor that can fit the request and reserves resources.
// Returns the assigned flavor name or an error if no flavor fits.
func (cq *ClusterQueue) AssignFlavor(w *Workload) (FlavorName, error) {
	req := w.TotalRequests()
	for _, fq := range cq.Flavors {
		if fq.CanFit(req) {
			fq.Reserve(req)
			w.FlavorName = fq.Name
			w.Status = StatusAdmitted
			now := time.Now()
			w.AdmittedAt = &now
			cq.Workloads = append(cq.Workloads, w)
			return fq.Name, nil
		}
	}
	return "", fmt.Errorf("no flavor fits: need %v, flavors: %d available", req, len(cq.Flavors))
}

// ReleaseWorkload frees flavor resources for a workload.
func (cq *ClusterQueue) ReleaseWorkload(w *Workload) {
	req := w.TotalRequests()
	for _, fq := range cq.Flavors {
		if fq.Name == w.FlavorName {
			fq.Release(req)
			break
		}
	}
	w.Status = StatusFinished
	now := time.Now()
	w.FinishedAt = &now
}

// Enqueue adds a workload to the pending queue and sorts by priority.
func (cq *ClusterQueue) Enqueue(w *Workload) {
	w.Status = StatusPending
	cq.Queue = append(cq.Queue, w)
	cq.sortQueue()
}

// sortQueue reorders the pending queue: higher priority first,
// then older workloads first (FIFO within same priority).
func (cq *ClusterQueue) sortQueue() {
	sort.SliceStable(cq.Queue, func(i, j int) bool {
		a, b := cq.Queue[i], cq.Queue[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority // higher first
		}
		return a.CreatedAt.Before(b.CreatedAt) // older first
	})
}

// Dequeue removes and returns the next workload (highest priority, earliest).
func (cq *ClusterQueue) Dequeue() *Workload {
	if len(cq.Queue) == 0 {
		return nil
	}
	w := cq.Queue[0]
	cq.Queue = cq.Queue[1:]
	return w
}

// Status returns per-flavor and queue state as a formatted string.
func (cq *ClusterQueue) Status() string {
	s := fmt.Sprintf("ClusterQueue: %s\n", cq.Name)
	for _, fq := range cq.Flavors {
		s += fmt.Sprintf("  Flavor[%s]: used=%v free=%v\n", fq.Name, fq.Used, fq.Available())
	}
	s += fmt.Sprintf("  Pending: %d workloads\n", len(cq.Queue))
	s += fmt.Sprintf("  Active:  %d workloads\n", len(cq.Workloads))
	return s
}

// ── LocalQueue ──────────────────────────────────────────────────

// LocalQueue is a namespace-scoped queue pointing to a ClusterQueue.
type LocalQueue struct {
	Name             string
	Namespace        string
	ClusterQueueName string
	Workloads        []*Workload
}
