// Package v1beta1 defines the Kubernetes API types for mini-kueue.
//
// CRDs defined here (4 total, down from kueue's 10+):
//   ClusterQueue    — cluster-scoped resource pool (cpu, mem, gpu quotas per flavor)
//   LocalQueue      — namespace-scoped queue that points to a ClusterQueue
//   Workload        — a job's resource request (PodSets + queue reference)
//   ResourceFlavor  — a resource flavor (e.g., "on-demand", "spot", "gpu-t4")
//
// Usage: run generate.sh to produce zz_generated.deepcopy.go and client code.
//
// Unlike real kueue, we do NOT implement: Cohort, AdmissionCheck, WorkloadPriorityClass,
// MultiKueueCluster, MultiKueueConfig, ProvisioningRequestConfig, Topology.
package v1beta1
