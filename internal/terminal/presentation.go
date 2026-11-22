// Package terminal formats explicitly human-facing messages. Protocol output,
// serialized metadata and application logs must bypass this package.
package terminal

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

type Style string

const (
	Error   Style = "31"
	Warning Style = "33"
	Success Style = "32"
	Heading Style = "1"
)

func supported(term string) bool {
	for _, name := range []string{"xterm", "screen", "tmux", "rxvt", "foot"} {
		if term == name || strings.HasPrefix(term, name+"-") {
			return true
		}
	}
	switch term {
	case "linux", "ansi", "vt100", "alacritty", "kitty", "wezterm":
		return true
	}
	return false
}

func enabled(out io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || !supported(os.Getenv("TERM")) {
		return false
	}
	f, ok := out.(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}

// Println checks the actual destination each time, including redirected stderr.
// Unsupported environments fall back to the original plain message.
func Println(out io.Writer, style Style, args ...any) {
	message := strings.TrimSuffix(fmt.Sprintln(args...), "\n")
	if enabled(out) {
		fmt.Fprintf(out, "\x1b[%sm%s\x1b[0m\n", style, message)
	} else {
		fmt.Fprintln(out, message)
	}
}

func Printf(out io.Writer, style Style, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if enabled(out) {
		fmt.Fprintf(out, "\x1b[%sm%s\x1b[0m", style, message)
	} else {
		fmt.Fprint(out, message)
	}
}
