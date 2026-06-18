// Package builder orchestrates Dockerfile builds via mini-containerd.
// For each instruction, it downloads the base image and executes commands.
//
// Note: RUN commands execute on the host for simplicity (like kaniko).
// A production implementation would create temporary containers via containerd.
package builder

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	containerd "github.com/containerd/containerd/v2/client"

	"github.com/tcherry/mini-container-ecosystem/mini-docker/internal/builder/parser"
	"github.com/tcherry/mini-container-ecosystem/mini-docker/pkg/registry"
)

// BuildResult holds the result of a Dockerfile build.
type BuildResult struct {
	ImageID string
	Layers  int
}

// Builder executes Dockerfile builds.
type Builder struct {
	client      *containerd.Client
	buildCtxDir string
}

// NewBuilder creates a new builder with an optional build context directory.
func NewBuilder(client *containerd.Client) *Builder {
	dir, _ := os.Getwd()
	return &Builder{client: client, buildCtxDir: dir}
}

// Build parses and executes a Dockerfile.
func (b *Builder) Build(ctx context.Context, dockerfile string, progress io.Writer, imageTag string) (*BuildResult, error) {
	instrs, err := parser.Parse(dockerfile)
	if err != nil {
		return nil, fmt.Errorf("parse Dockerfile: %w", err)
	}

	var currentImage string
	layerCount := 0

	for _, instr := range instrs {
		fmt.Fprintf(progress, "Step %d/%d : %s\n", instr.Line, len(instrs), instr.Raw)

		switch instr.Type {
		case parser.InstrFROM:
			ref := instr.Args[0]
			fmt.Fprintf(progress, " ---> Pulling %s\n", ref)
			if err := registry.Pull(ctx, b.client, ref); err != nil {
				return nil, fmt.Errorf("pull %s: %w", ref, err)
			}
			currentImage = ref
			layerCount++
			fmt.Fprintf(progress, " ---> %s\n", currentImage)

		case parser.InstrRUN:
			if currentImage == "" {
				return nil, fmt.Errorf("no base image for RUN")
			}
			cmdStr := instr.Args[0]
			fmt.Fprintf(progress, " ---> Running in %s: %s\n", currentImage, cmdStr)

			// Execute in temporary dir to simulate container filesystem
			tmpDir, _ := os.MkdirTemp("", "mini-docker-build-*")
			cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
			cmd.Dir = tmpDir
			cmd.Stdout = progress
			cmd.Stderr = progress
			if err := cmd.Run(); err != nil {
				os.RemoveAll(tmpDir)
				return nil, fmt.Errorf("RUN %q: %w", cmdStr, err)
			}
			os.RemoveAll(tmpDir)
			layerCount++

		case parser.InstrCOPY:
			if currentImage == "" {
				return nil, fmt.Errorf("no base image for COPY")
			}
			if len(instr.Args) >= 2 {
				src := instr.Args[0]
				dst := instr.Args[1]
				fmt.Fprintf(progress, " ---> COPY %s -> %s\n", src, dst)
				// Execute cp from build context
				cpCmd := exec.CommandContext(ctx, "cp", "-r",
					b.buildCtxDir+"/"+src, dst)
				if err := cpCmd.Run(); err != nil {
					return nil, fmt.Errorf("COPY %s: %w", src, err)
				}
			}
			layerCount++

		case parser.InstrENV, parser.InstrWORKDIR, parser.InstrCMD, parser.InstrEXPOSE, parser.InstrLABEL:
			fmt.Fprintf(progress, " ---> %s (metadata only)\n", instr.Type)
		}
	}

	imageID := currentImage
	if imageTag != "" {
		imageID = imageTag
	}
	return &BuildResult{ImageID: imageID, Layers: layerCount}, nil
}
