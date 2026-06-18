// Package parser implements a minimal Dockerfile parser for mini-docker.
//
// Supported instructions:
//   FROM <image>            — base image
//   RUN <command>           — execute command in a new layer (creates temp container)
//   COPY <src> <dst>        — copy files from build context into image
//   CMD <command>           — default container command
//   ENTRYPOINT <command>    — container entrypoint
//   ENV <key>=<value>       — set environment variable
//   WORKDIR <path>          — set working directory
//   EXPOSE <port>           — document exposed port
//   LABEL <key>=<value>     — add metadata label
//
// Not supported (compared to real Docker):
//   ARG, ADD, VOLUME, USER, HEALTHCHECK, SHELL, STOPSIGNAL, ONBUILD
package parser
