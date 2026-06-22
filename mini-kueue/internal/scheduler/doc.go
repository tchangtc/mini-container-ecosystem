// Package scheduler implements the core workload scheduling loop for mini-kueue.
//
// Architecture (simplified from real kueue's 3-package scheduler):
//
//  1. Queue Manager watches LocalQueue → ClusterQueue linkage and maintains
//     a FIFO queue of pending Workloads per ClusterQueue.
//
//  2. Flavor Assigner checks if the ClusterQueue has enough resources to admit
//     the head-of-line workload. It assigns a single ResourceFlavor to each PodSet.
//     No borrowing, no topology-aware assignment — just simple quota arithmetic.
//
//  3. Scheduler loop: on every Workload create/update event, iterate over
//     all ClusterQueues and admit as many pending workloads as quota permits.
//
//     for each ClusterQueue:
//         while head of queue fits in remaining quota:
//             admit(head)
//             remaining -= head.resources
//
// What we do NOT implement (unlike real kueue):
//   - FairSharing (DRF-based weighted fair queueing)
//   - Preemption (evicting lower-priority workloads)
//   - Borrowing (lending unused quota to other queues in the cohort)
//   - TAS (Topology-Aware Scheduling)
//   - Multiple flavors per PodSet
package scheduler
