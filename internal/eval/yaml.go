package eval

import (
	"fmt"
	"strings"
)

type line struct {
	num    int
	indent int
	text   string
}

type parser struct {
	lines []line
	pos   int
}

func parseYAML(src string) (map[string]any, error) {
	lines, err := splitLines(src)
	if err != nil {
		return nil, err
	}
	p := &parser{lines: lines}
	m, err := p.parseMap(0)
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.lines) {
		return nil, lineErr(p.lines[p.pos], "unexpected indent")
	}
	return m, nil
}

func splitLines(src string) ([]line, error) {
	var lines []line
	for i, raw := range strings.Split(src, "\n") {
		l := line{num: i + 1}
		body := strings.TrimLeft(raw, " \t")
		if strings.Contains(raw[:len(raw)-len(body)], "\t") {
			return nil, lineErr(l, "tabs are not allowed, indent with spaces")
		}
		l.indent = len(raw) - len(body)
		l.text = strings.TrimRight(stripComment(body), " ")
		if l.text != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func (p *parser) parseBlock(indent int) (any, error) {
	if isListItem(p.lines[p.pos].text) {
		return p.parseList(indent)
	}
	return p.parseMap(indent)
}

func (p *parser) parseMap(indent int) (map[string]any, error) {
	m := map[string]any{}
	for p.pos < len(p.lines) {
		l := p.lines[p.pos]
		if l.indent < indent {
			break
		}
		if l.indent > indent {
			return nil, lineErr(l, "unexpected indent")
		}
		key, rest, ok := splitKey(l.text)
		if !ok {
			return nil, lineErr(l, `expected "key: value"`)
		}
		if _, dup := m[key]; dup {
			return nil, lineErr(l, "duplicate key %q", key)
		}
		p.pos++

		var v any
		var err error
		switch {
		case rest != "":
			v, err = parseScalar(rest, l)
		case p.pos < len(p.lines) && p.lines[p.pos].indent > indent:
			v, err = p.parseBlock(p.lines[p.pos].indent)
		case p.pos < len(p.lines) && p.lines[p.pos].indent == indent && isListItem(p.lines[p.pos].text):
			v, err = p.parseList(indent)
		default:
			v = ""
		}
		if err != nil {
			return nil, err
		}
		m[key] = v
	}
	return m, nil
}

func (p *parser) parseList(indent int) ([]any, error) {
	list := []any{}
	for p.pos < len(p.lines) {
		l := p.lines[p.pos]
		if l.indent < indent || (l.indent == indent && !isListItem(l.text)) {
			break
		}
		if l.indent > indent {
			return nil, lineErr(l, "unexpected indent")
		}

		rest := strings.TrimLeft(l.text[1:], " ")
		var v any
		var err error
		if rest == "" {
			p.pos++
			v = ""
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				v, err = p.parseBlock(p.lines[p.pos].indent)
			}
		} else if _, _, ok := splitKey(rest); ok {
			itemIndent := indent + len(l.text) - len(rest)
			p.lines[p.pos] = line{num: l.num, indent: itemIndent, text: rest}
			v, err = p.parseMap(itemIndent)
		} else {
			p.pos++
			v, err = parseScalar(rest, l)
		}
		if err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, nil
}

func parseScalar(s string, l line) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	switch s[0] {
	case '|', '>':
		return nil, lineErr(l, "| and > text blocks are not supported, use a quoted string")
	case '&', '*', '!':
		return nil, lineErr(l, "anchors, aliases and tags are not supported")
	case '[':
		if !strings.HasSuffix(s, "]") {
			return nil, lineErr(l, "unclosed [")
		}
		list := []any{}
		for _, item := range splitTopLevel(s[1 : len(s)-1]) {
			v, err := parseScalar(item, l)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	case '{':
		if !strings.HasSuffix(s, "}") {
			return nil, lineErr(l, "unclosed {")
		}
		m := map[string]any{}
		for _, item := range splitTopLevel(s[1 : len(s)-1]) {
			key, rest, ok := splitKey(item)
			if !ok {
				return nil, lineErr(l, `expected "key: value" inside { }`)
			}
			if _, dup := m[key]; dup {
				return nil, lineErr(l, "duplicate key %q", key)
			}
			v, err := parseScalar(rest, l)
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
		return m, nil
	case '"', '\'':
		return unquote(s, l)
	}
	return s, nil
}

func unquote(s string, l line) (string, error) {
	q := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '"' && c == '\\' && i+1 < len(s):
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(s[i])
			}
		case q == '\'' && c == '\'' && i+1 < len(s) && s[i+1] == '\'':
			i++
			b.WriteByte('\'')
		case c == q:
			if strings.TrimSpace(s[i+1:]) != "" {
				return "", lineErr(l, "unexpected text after closing quote")
			}
			return b.String(), nil
		default:
			b.WriteByte(c)
		}
	}
	return "", lineErr(l, "unclosed quote")
}

type charState struct {
	quoted bool
	depth  int
}

func scan(s string) []charState {
	states := make([]charState, len(s))
	var quote byte
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			states[i] = charState{true, depth}
			switch {
			case quote == '"' && c == '\\' && i+1 < len(s):
				i++
				states[i] = charState{true, depth}
			case quote == '\'' && c == '\'' && i+1 < len(s) && s[i+1] == '\'':
				i++
				states[i] = charState{true, depth}
			case c == quote:
				quote = 0
			}
			continue
		}
		if (c == '"' || c == '\'') && (i == 0 || strings.IndexByte(" [{,", s[i-1]) >= 0) {
			quote = c
			states[i] = charState{true, depth}
			continue
		}
		if c == '[' || c == '{' {
			depth++
		}
		states[i] = charState{false, depth}
		if c == ']' || c == '}' {
			depth--
		}
	}
	return states
}

func stripComment(s string) string {
	for i, st := range scan(s) {
		if s[i] == '#' && !st.quoted && (i == 0 || s[i-1] == ' ') {
			return s[:i]
		}
	}
	return s
}

func splitKey(s string) (key, rest string, ok bool) {
	for i, st := range scan(s) {
		if s[i] != ':' || st.quoted || st.depth != 0 {
			continue
		}
		if i+1 < len(s) && s[i+1] != ' ' {
			continue
		}
		key = strings.TrimSpace(s[:i])
		if key == "" || isListItem(key) {
			return "", "", false
		}
		return key, strings.TrimSpace(s[i+1:]), true
	}
	return "", "", false
}

func splitTopLevel(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var parts []string
	start := 0
	for i, st := range scan(s) {
		if s[i] == ',' && !st.quoted && st.depth == 0 {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

func isListItem(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

func lineErr(l line, format string, args ...any) error {
	return fmt.Errorf("line %d: "+format, append([]any{l.num}, args...)...)
}
