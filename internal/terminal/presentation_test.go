package terminal

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/creack/pty"
)

func TestPresentationPerStreamAndPlainFallback(t *testing.T) {
	for _, term := range []string{"xterm-256color", "dumb", "", "unknown-terminal"} {
		for _, noColor := range []bool{false, true} {
			t.Run(term+map[bool]string{false: "", true: "-NO_COLOR"}[noColor], func(t *testing.T) {
				t.Setenv("TERM", term)
				// An empty NO_COLOR value must disable styling too.
				t.Setenv("NO_COLOR", "")
				if !noColor {
					os.Unsetenv("NO_COLOR")
				}
				master, slave, err := pty.Open()
				if err != nil {
					t.Fatal(err)
				}
				defer master.Close()
				defer slave.Close()
				Println(slave, Error, "blocked", 7)
				line, err := bufio.NewReader(master).ReadString('\n')
				if err != nil {
					t.Fatal(err)
				}
				colour := term == "xterm-256color" && !noColor
				if strings.Contains(line, "\x1b[31m") != colour || !strings.Contains(line, "blocked 7") {
					t.Fatal(line)
				}
				var redirected bytes.Buffer
				Println(&redirected, Success, "confirmed")
				Printf(&redirected, Heading, "next %d\n", 2)
				if redirected.String() != "confirmed\nnext 2\n" {
					t.Fatal(redirected.String())
				}
			})
		}
	}
}
