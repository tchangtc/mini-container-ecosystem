// mini-kueue controller — priority-aware, flavor-based workload scheduler.
// Demonstrates Kueue's scheduling logic without K8s dependencies.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/tcherry/mini-container-ecosystem/mini-kueue/pkg/scheduler"
	"github.com/tcherry/mini-container-ecosystem/mini-kueue/pkg/types"
)

var sched = scheduler.New()

func main() {
	// dev-queue: two flavors — standard (CPU-only) and gpu (with GPUs)
	dev := &types.ClusterQueue{
		Name: "dev-queue",
		Flavors: []*types.FlavorQuota{
			{Name: "standard", Resources: types.ResourceList{types.ResourceCPU: 10, types.ResourceMemory: 32}},
			{Name: "gpu", Resources: types.ResourceList{types.ResourceCPU: 8, types.ResourceMemory: 64, types.ResourceGPU: 4}},
		},
	}
	sched.AddClusterQueue(dev)

	// prod-queue: single large flavor
	prod := &types.ClusterQueue{
		Name: "prod-queue",
		Flavors: []*types.FlavorQuota{
			{Name: "on-demand", Resources: types.ResourceList{types.ResourceCPU: 100, types.ResourceMemory: 256, types.ResourceGPU: 8}},
		},
	}
	sched.AddClusterQueue(prod)

	sched.AddLocalQueue(&types.LocalQueue{Name: "default", Namespace: "default", ClusterQueueName: "dev-queue"})
	sched.AddLocalQueue(&types.LocalQueue{Name: "ml-training", Namespace: "ml-team", ClusterQueueName: "dev-queue"})
	sched.AddLocalQueue(&types.LocalQueue{Name: "production", Namespace: "default", ClusterQueueName: "prod-queue"})

	http.HandleFunc("/submit", handleSubmit)
	http.HandleFunc("/schedule", handleSchedule)
	http.HandleFunc("/finish", handleFinish)
	http.HandleFunc("/status", handleStatus)

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	fmt.Printf("mini-kueue scheduler on :%s (flavor-aware + priority)\n", port)
	fmt.Println("  POST /submit  POST /schedule  POST /finish  GET /status")
	http.ListenAndServe(":"+port, nil)
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string             `json:"name"`
		Namespace string             `json:"namespace"`
		QueueName string             `json:"queueName"`
		Resources types.ResourceList `json:"resources"`
		PodCount  int                `json:"podCount"`
		Priority  int32              `json:"priority"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Namespace == "" {
		req.Namespace = "default"
	}
	if req.PodCount == 0 {
		req.PodCount = 1
	}
	wl := types.NewWorkload(req.Name, req.Namespace, req.QueueName, req.Resources, req.PodCount, req.Priority)
	if err := sched.Submit(wl); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "submitted", "name": wl.Name, "priority": wl.Priority,
	})
}

func handleSchedule(w http.ResponseWriter, r *http.Request) {
	admitted := sched.Schedule()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"admitted": len(admitted), "workloads": admitted,
	})
}

func handleFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		QueueName string `json:"queueName"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if err := sched.FinishByName(req.Namespace, req.QueueName, req.Name); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "finished"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(sched.Status()))
}
