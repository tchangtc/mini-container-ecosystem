// Package container implements container lifecycle operations for mini-nerdctl.
// It wraps containerd's Container and Task objects to provide:
//   - Create: new container from image + OCI spec
//   - List (ps): query all containers in the namespace
//   - Start: begin execution of a created container
//   - Stop: send SIGTERM/SIGKILL and wait for exit
//   - Delete (rm): remove container and its resources
package container
