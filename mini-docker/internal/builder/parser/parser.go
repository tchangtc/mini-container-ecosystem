// Package parser implements a minimal Dockerfile parser.
// Supports: FROM, RUN, COPY, CMD, ENTRYPOINT, ENV, WORKDIR, EXPOSE, LABEL.
package parser

import (
	"fmt"
	"strings"
)

// Instruction types
const (
	InstrFROM       = "FROM"
	InstrRUN        = "RUN"
	InstrCOPY       = "COPY"
	InstrCMD        = "CMD"
	InstrENTRYPOINT = "ENTRYPOINT"
	InstrENV        = "ENV"
	InstrWORKDIR    = "WORKDIR"
	InstrEXPOSE     = "EXPOSE"
	InstrLABEL      = "LABEL"
	InstrCOMMENT    = "#"
)

// Instruction is a single parsed Dockerfile line.
type Instruction struct {
	Type string
	Args []string // shell-parsed arguments
	Raw  string   // original line
	Line int      // line number
}

// Parse parses a Dockerfile string into a list of instructions.
func Parse(dockerfile string) ([]Instruction, error) {
	var instructions []Instruction
	lines := strings.Split(dockerfile, "\n")

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Handle line continuation (\)
		for strings.HasSuffix(line, "\\") {
			line = line[:len(line)-1]
			i++
			if i >= len(lines) {
				break
			}
			line += " " + strings.TrimSpace(lines[i])
		}

		instr, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		instr.Line = i + 1
		instructions = append(instructions, instr)
	}
	return instructions, nil
}

// parseLine parses a single line into an instruction.
func parseLine(line string) (Instruction, error) {
	instr := Instruction{Raw: line}
	parts := strings.SplitN(line, " ", 2)
	instr.Type = strings.ToUpper(parts[0])

	switch instr.Type {
	case InstrFROM, InstrRUN, InstrCOPY:
		if len(parts) < 2 {
			return instr, fmt.Errorf("%s requires arguments", instr.Type)
		}
		instr.Args = shellSplit(parts[1])
	case InstrCMD, InstrENTRYPOINT:
		if len(parts) > 1 {
			instr.Args = shellSplit(parts[1])
		}
	case InstrENV:
		if len(parts) > 1 {
			instr.Args = shellSplit(parts[1])
		}
	case InstrWORKDIR:
		if len(parts) > 1 {
			instr.Args = []string{parts[1]}
		}
	case InstrEXPOSE:
		if len(parts) > 1 {
			instr.Args = strings.Fields(parts[1])
		}
	case InstrLABEL:
		if len(parts) > 1 {
			instr.Args = []string{parts[1]}
		}
	default:
		return instr, fmt.Errorf("unknown instruction: %s", instr.Type)
	}
	return instr, nil
}

// shellSplit splits a string respecting quoted strings.
// e.g. `echo "hello world" foo` → ["echo", "hello world", "foo"]
func shellSplit(s string) []string {
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for _, c := range s {
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == ' ' && !inSingle && !inDouble:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(c)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
