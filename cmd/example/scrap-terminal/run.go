// scrap-terminal hosts the terminal platform: it runs a program over the
// lines of standard input, and prints its output.
package main

//go:generate go run ../../scrap-gen-go program.scrap

import (
	"bufio"
	"io"
	"os"
	"strings"
)

func run(p Program, args []string) (int, error) {
	return Run(p, os.Stdin, os.Stdout)
}

// Run runs the program p, reading lines from r and writing its output to w,
// until it asks to exit, and returns its status. Reads after the last line
// keep returning #eof, as with POSIX read; a last line without a newline is
// still a line.
func Run(p Program, r io.Reader, w io.Writer) (int, error) {
	in := bufio.NewReader(r)
	eof := false
	turn := p.Init
	for {
		if _, err := io.WriteString(w, turn.Out); err != nil {
			return 0, err
		}
		if turn.Next.Tag() == NextActionExit {
			return int(turn.Next.Exit()), nil
		}

		event := NewEventEof()
		if !eof {
			line, err := in.ReadString('\n')
			switch {
			case err == io.EOF:
				eof = true
				if line != "" {
					event = NewEventLine(line)
				}
			case err != nil:
				return 0, err
			default:
				event = NewEventLine(strings.TrimSuffix(line, "\n"))
			}
		}

		var err error
		if turn, err = p.Step(event, turn.State); err != nil {
			return 0, err
		}
	}
}
