package maintenancetask

// cronCommand preserves the provider's two distinct translations: backslash
// parity determines the first command/input separator, while stdin preserves
// non-percent escapes. See Cronie 1.7.1 src/do_command.c (also Vixie cron).
func cronCommand(raw string) (string, string) {
	command := make([]byte, 0, len(raw))
	escaped := false
	inputStart := len(raw)
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			if ch == '%' {
				command = command[:len(command)-1]
			}
			command = append(command, ch)
			escaped = false
			continue
		}
		if ch == '%' {
			inputStart = i + 1
			break
		}
		command = append(command, ch)
		escaped = ch == '\\'
	}
	input := make([]byte, 0, len(raw)-inputStart+1)
	escaped = false
	needNewline := false
	for i := inputStart; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			if ch != '%' {
				input = append(input, '\\')
			}
		} else if ch == '%' {
			ch = '\n'
		}
		escaped = ch == '\\'
		if !escaped {
			input = append(input, ch)
			needNewline = ch != '\n'
		}
	}
	if escaped {
		input = append(input, '\\')
	}
	if needNewline {
		input = append(input, '\n')
	}
	return string(command), string(input)
}
