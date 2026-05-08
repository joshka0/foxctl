package embeddingtext

import "strings"

func cleanSlashCommentSource(code string) string {
	return compactBlankLines(stripSlashComments(code))
}

func cleanHashCommentSource(code string) string {
	return compactBlankLines(stripHashComments(code))
}

func cleanPythonSource(code string) string {
	return compactBlankLines(stripHashComments(stripPythonDocstrings(code)))
}

func stripSlashComments(code string) string {
	var out strings.Builder
	inString := rune(0)
	escaped := false
	for i := 0; i < len(code); i++ {
		ch := rune(code[i])
		if inString != 0 {
			out.WriteByte(code[i])
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			out.WriteByte(code[i])
			continue
		}
		if ch == '/' && i+1 < len(code) {
			next := code[i+1]
			if next == '/' {
				for i < len(code) && code[i] != '\n' {
					i++
				}
				if i < len(code) {
					out.WriteByte('\n')
				}
				continue
			}
			if next == '*' {
				i += 2
				for i+1 < len(code) && !(code[i] == '*' && code[i+1] == '/') {
					if code[i] == '\n' {
						out.WriteByte('\n')
					}
					i++
				}
				i++
				continue
			}
		}
		out.WriteByte(code[i])
	}
	return out.String()
}

func stripHashComments(code string) string {
	lines := strings.Split(code, "\n")
	for i, line := range lines {
		lines[i] = stripHashCommentLine(line)
	}
	return strings.Join(lines, "\n")
}

func stripHashCommentLine(line string) string {
	var out strings.Builder
	inString := rune(0)
	escaped := false
	for _, ch := range line {
		if inString != 0 {
			out.WriteRune(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = ch
			out.WriteRune(ch)
			continue
		}
		if ch == '#' {
			break
		}
		out.WriteRune(ch)
	}
	return strings.TrimRight(out.String(), " \t")
}

func stripPythonDocstrings(code string) string {
	code = stripDelimitedText(code, `"""`)
	code = stripDelimitedText(code, `'''`)
	return code
}

func stripDelimitedText(code, delimiter string) string {
	for {
		start := strings.Index(code, delimiter)
		if start < 0 {
			return code
		}
		end := strings.Index(code[start+len(delimiter):], delimiter)
		if end < 0 {
			return code
		}
		end += start + len(delimiter)
		replacement := strings.Repeat("\n", strings.Count(code[start:end+len(delimiter)], "\n"))
		code = code[:start] + replacement + code[end+len(delimiter):]
	}
}

func compactBlankLines(code string) string {
	lines := strings.Split(code, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, line)
		blank = false
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
