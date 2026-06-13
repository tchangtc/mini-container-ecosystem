// Package cmd provides the CLI command definitions for mini-nerdctl.
// Commands are registered via cobra and delegate to the pkg/ implementations.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/spf13/cobra"

	"github.com/tcherry/mini-container-ecosystem/mini-nerdctl/pkg/config"
	"github.com/tcherry/mini-container-ecosystem/mini-nerdctl/pkg/container"
	"github.com/tcherry/mini-container-ecosystem/mini-nerdctl/pkg/image"
	"github.com/tcherry/mini-container-ecosystem/mini-nerdctl/pkg/runtime"
)

var (
	cfg = config.FromEnv()
	cli *containerd.Client
)

// getClient returns a lazily-initialized containerd client.
// The client is reused across commands within a single CLI invocation.
func getClient() (*containerd.Client, error) {
	if cli != nil {
		return cli, nil
	}
	var err error
	cli, err = containerd.New(cfg.Address, containerd.WithDefaultNamespace(cfg.Namespace))
	if err != nil {
		return nil, fmt.Errorf("connect to containerd at %s: %w", cfg.Address, err)
	}
	return cli, nil
}

// signalContext returns a context that cancels on SIGINT/SIGTERM.
func signalContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()
	return ctx
}

// ── Root command ─────────────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:   "mini-nerdctl",
	Short: "A minimal containerd CLI (8 commands)",
	Long: `mini-nerdctl is a simplified implementation of nerdctl (nerdctl → 8 commands).
It wraps containerd's gRPC API to provide basic container lifecycle management.

Commands: pull, images, rmi, run, ps, exec, logs, stop, rm`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Validate connection to containerd before running any command
		_, err := getClient()
		return err
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		if cli != nil {
			return cli.Close()
		}
		return nil
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// ── Image commands ───────────────────────────────────────────────

var pullCmd = &cobra.Command{
	Use:   "pull <image>",
	Short: "Pull an image from a registry and store it locally",
	Long:  `Pull downloads an OCI image from a registry (default: docker.io) and unpacks it for use as a container rootfs.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		client, err := getClient()
		if err != nil {
			return err
		}
		return image.Pull(ctx, client, args[0], os.Stderr)
	},
}

var imagesCmd = &cobra.Command{
	Use:   "images",
	Short: "List locally stored images",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		client, err := getClient()
		if err != nil {
			return err
		}
		imgs, err := image.List(ctx, client)
		if err != nil {
			return err
		}
		if len(imgs) == 0 {
			fmt.Println("No images found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 4, 8, 2, ' ', 0)
		fmt.Fprintln(w, "IMAGE\tDIGEST\tSIZE\tCREATED")
		for _, img := range imgs {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
				img.Name,
				shortDigest(img.Digest),
				img.Size,
				img.CreatedAt.Format(time.RFC3339),
			)
		}
		return w.Flush()
	},
}

var rmiCmd = &cobra.Command{
	Use:   "rmi <image>",
	Short: "Remove a locally stored image",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		client, err := getClient()
		if err != nil {
			return err
		}
		if err := image.Remove(ctx, client, args[0]); err != nil {
			return err
		}
		fmt.Printf("Deleted: %s\n", args[0])
		return nil
	},
}

// ── Container commands ───────────────────────────────────────────

var (
	runInteractive bool
	runTTY         bool
	runDetach      bool
	runRm          bool
	runEnv         []string
)

var runCmd = &cobra.Command{
	Use:   "run <image> [command...]",
	Short: "Create and run a new container",
	Long: `Run creates a container from the specified image and executes the given command.
If no command is provided, the image's default entrypoint/cmd is used.

By default, the container runs in the foreground and is automatically removed on exit.
Use -d to run in detached mode (background).`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		client, err := getClient()
		if err != nil {
			return err
		}

		opts := container.RunOpts{
			Image:       args[0],
			Cmd:         args[1:],
			Interactive: runInteractive,
			TTY:         runTTY,
			Detach:      runDetach,
			Rm:          runRm || !runDetach, // default --rm for foreground
			Env:         runEnv,
		}

		id, exitCode, err := container.Run(ctx, client, opts, os.Stdout, os.Stderr)
		if err != nil {
			return err
		}

		if runDetach {
			fmt.Println(id)
		} else {
			os.Exit(exitCode)
		}
		return nil
	},
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List containers",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		client, err := getClient()
		if err != nil {
			return err
		}

		containers, err := container.List(ctx, client)
		if err != nil {
			return err
		}

		if len(containers) == 0 {
			fmt.Println("No containers found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 4, 8, 2, ' ', 0)
		fmt.Fprintln(w, "CONTAINER ID\tIMAGE\tSTATUS\tPID")
		for _, c := range containers {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\n",
				shortID(c.ID),
				c.Image,
				c.Status,
				c.Pid,
			)
		}
		return w.Flush()
	},
}

var execCmd = &cobra.Command{
	Use:   "exec <container-id> <command> [args...]",
	Short: "Execute a command in a running container",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		client, err := getClient()
		if err != nil {
			return err
		}

		exitCode, err := runtime.Exec(ctx, client, args[0], args[1:], false, os.Stdin, os.Stdout, os.Stderr)
		if err != nil {
			return err
		}
		os.Exit(exitCode)
		return nil
	},
}

var logsCmd = &cobra.Command{
	Use:   "logs <container-id>",
	Short: "Fetch logs of a container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		client, err := getClient()
		if err != nil {
			return err
		}

		reader, err := runtime.Logs(ctx, client, args[0])
		if err != nil {
			return err
		}
		defer reader.Close()

		if _, err := io.Copy(os.Stdout, reader); err != nil {
			return fmt.Errorf("read logs: %w", err)
		}
		return nil
	},
}

var (
	stopTimeout time.Duration
)

var stopCmd = &cobra.Command{
	Use:   "stop <container-id>",
	Short: "Stop a running container (SIGTERM, then SIGKILL after timeout)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		client, err := getClient()
		if err != nil {
			return err
		}

		if err := container.Stop(ctx, client, args[0], stopTimeout); err != nil {
			return err
		}
		fmt.Printf("Stopped: %s\n", args[0])
		return nil
	},
}

var (
	rmForce bool
)

var rmCmd = &cobra.Command{
	Use:   "rm <container-id>",
	Short: "Remove a container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := signalContext()
		client, err := getClient()
		if err != nil {
			return err
		}

		if err := container.Remove(ctx, client, args[0], rmForce); err != nil {
			return err
		}
		fmt.Printf("Deleted: %s\n", args[0])
		return nil
	},
}

// ── Helpers ──────────────────────────────────────────────────────

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func shortDigest(d string) string {
	// "sha256:abc123def..." → "sha256:abc123de"
	if len(d) > 19 {
		return d[:19]
	}
	return d
}

// ── Command registration ─────────────────────────────────────────

func init() {
	// run flags
	runCmd.Flags().BoolVarP(&runInteractive, "interactive", "i", false, "Keep stdin open")
	runCmd.Flags().BoolVarP(&runTTY, "tty", "t", false, "Allocate a pseudo-TTY")
	runCmd.Flags().BoolVarP(&runDetach, "detach", "d", false, "Run container in background")
	runCmd.Flags().BoolVar(&runRm, "rm", false, "Remove container on exit")
	runCmd.Flags().StringArrayVarP(&runEnv, "env", "e", nil, "Set environment variables (KEY=VALUE)")

	// stop flags
	stopCmd.Flags().DurationVarP(&stopTimeout, "timeout", "t", 10*time.Second, "Seconds to wait before force-kill")

	// rm flags
	rmCmd.Flags().BoolVarP(&rmForce, "force", "f", false, "Force removal (stop if running)")

	// Register all commands
	rootCmd.AddCommand(pullCmd)
	rootCmd.AddCommand(imagesCmd)
	rootCmd.AddCommand(rmiCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(rmCmd)
}
