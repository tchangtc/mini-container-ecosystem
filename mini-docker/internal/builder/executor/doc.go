// Package executor implements Dockerfile instruction execution for mini-docker.
//
// The build process:
//  1. Parse Dockerfile into a list of instructions
//  2. Pull the base image (FROM)
//  3. For each subsequent instruction:
//     a. Create a temporary container from the previous layer
//     b. Execute the instruction (RUN → run command, COPY → write files, etc.)
//     c. Commit the container as a new snapshot layer
//  4. Tag the final layer as the output image
//
// Build cache: each instruction is hashed (instruction text + input files for COPY).
// If the same instruction with the same inputs has been executed before, reuse the
// cached layer instead of re-executing.
package executor
