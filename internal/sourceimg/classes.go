// Package sourceimg reads C++ class declarations out of developer images.
//
// Arcade boards in the NX fleet carry their build tree in an EEPROM window, so
// a dump often contains plain C++ rather than compiled code. This parser only
// needs enough C++ to find top-level class/struct bodies and their members; it
// is not a compiler.
package sourceimg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrNotText = errors.New("image is not C++ text")

type Field struct {
	Type    string
	Name    string
	Default *int
}

type Class struct {
	Name    string
	Start   int
	End     int
	Line    int
	Fields  []Field
	Methods []string
	Digest  string
}

var fieldHead = regexp.MustCompile(`(?s)^((?:static\s+|const\s+|constexpr\s+|mutable\s+|volatile\s+|thread_local\s+)*)((?:unsigned(?:\s+(?:char|short|int|long))?|signed(?:\s+(?:char|short|int|long))?|long\s+long|long\s+double|int|long|short|char|bool|float|double|size_t|ssize_t|u?int(?:8|16|32|64)_t|std::u?int(?:8|16|32|64)_t))\s+(.+)$`)

var methodStop = map[string]bool{
	"alignof": true, "catch": true, "decltype": true, "delete": true,
	"do": true, "else": true, "for": true, "if": true, "new": true,
	"noexcept": true, "operator": true, "return": true, "sizeof": true,
	"static_assert": true, "switch": true, "throw": true, "try": true,
	"typeof": true, "while": true,
}

var methodQualifier = map[string]bool{
	"const": true, "constexpr": true, "consteval": true, "final": true,
	"noexcept": true, "override": true, "requires": true, "throw": true,
	"try": true,
}

// Parse returns every top-level class and struct it can account for. Malformed
// declarations are reported as warnings and skipped, which keeps a truncated
// EEPROM image from discarding the intact symbols around it.
func Parse(blob []byte) ([]Class, []string, error) {
	text := unpadded(blob)
	if len(text) == 0 {
		return nil, nil, ErrNotText
	}
	if !utf8.Valid(text) {
		return nil, nil, ErrNotText
	}

	structural := mask(text, false)
	withLits := mask(text, true)

	var (
		classes  []Class
		warnings []string
	)
	for i := 0; i < len(structural); {
		keyword, keywordLen := classKeywordAt(structural, i)
		if keyword == "" {
			i++
			continue
		}
		if keyword == "class" && precededByEnum(structural, i) {
			i += keywordLen
			continue
		}

		nameAt := skipSpace(structural, i+keywordLen)
		nameEnd := identifierEnd(structural, nameAt)
		if nameEnd == nameAt {
			i += keywordLen
			continue
		}
		name := string(structural[nameAt:nameEnd])

		after := skipSpace(structural, nameEnd)
		if wordAt(structural, after, "final") {
			after = skipSpace(structural, after+len("final"))
		}
		if after+1 < len(structural) && structural[after] == '[' && structural[after+1] == '[' {
			if end := bytes.Index(structural[after+2:], []byte("]]")); end >= 0 {
				after = skipSpace(structural, after+2+end+2)
			}
		}
		if after >= len(structural) {
			break
		}

		switch structural[after] {
		case '{', ':', ';':
			// plausible class head
		default:
			i = nameEnd
			continue
		}

		open := bytes.IndexByte(structural[after:], '{')
		if open < 0 {
			i = nameEnd
			continue
		}
		open += after
		semi := bytes.IndexByte(structural[after:], ';')
		if semi >= 0 && after+semi < open {
			i = nameEnd // forward declaration
			continue
		}

		close := closingBrace(structural, open)
		if close < 0 {
			line := 1 + bytes.Count(text[:i], []byte("\n"))
			warnings = append(warnings, fmt.Sprintf("class %s (line %d) has no closing brace; skipped", name, line))
			break
		}

		end := close + 1
		if tail := skipSpace(structural, close+1); tail < len(structural) && structural[tail] == ';' {
			end = tail + 1
		}

		body := structural[open+1 : close]
		digest := sha256.Sum256(normalize(withLits[i:end]))
		classes = append(classes, Class{
			Name:    name,
			Start:   i,
			End:     end,
			Line:    1 + bytes.Count(text[:i], []byte("\n")),
			Fields:  memberFields(body),
			Methods: memberMethods(body),
			Digest:  hex.EncodeToString(digest[:]),
		})
		i = end
	}

	return classes, warnings, nil
}

func unpadded(blob []byte) []byte {
	end := len(blob)
	for end > 0 && (blob[end-1] == 0 || blob[end-1] == 0xff) {
		end--
	}
	return blob[:end]
}

type scanState int

const (
	normal scanState = iota
	lineComment
	blockComment
	stringLiteral
	charLiteral
	rawLiteral
)

// mask blanks comments for structural analysis. When keepLiterals is false it
// also blanks string and character contents so braces inside them are inert.
func mask(src []byte, keepLiterals bool) []byte {
	out := append([]byte(nil), src...)
	const (
		empty = byte(' ')
	)
	state := normal
	rawTerm := []byte(nil)

	for i := 0; i < len(src); i++ {
		switch state {
		case normal:
			switch {
			case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
				out[i], out[i+1] = empty, empty
				state = lineComment
				i++
			case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
				out[i], out[i+1] = empty, empty
				state = blockComment
				i++
			case src[i] == '"':
				if !keepLiterals {
					out[i] = empty
				}
				state = stringLiteral
			case src[i] == '\'':
				if !keepLiterals {
					out[i] = empty
				}
				state = charLiteral
			case src[i] == 'R' && i+1 < len(src) && src[i+1] == '"' && (i == 0 || !identByte(src[i-1])):
				if delim, body, ok := rawOpen(src, i); ok {
					rawTerm = []byte(")" + string(delim) + `"`)
					if !keepLiterals {
						out[i] = empty
						out[i+1] = empty
						for k := i + 2; k <= body; k++ {
							if src[k] != '\n' && src[k] != '\r' {
								out[k] = empty
							}
						}
					}
					i = body
					state = rawLiteral
				}
			}

		case lineComment:
			if src[i] == '\n' {
				state = normal
				continue
			}
			out[i] = empty

		case blockComment:
			if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
				out[i], out[i+1] = empty, empty
				state = normal
				i++
				continue
			}
			if src[i] != '\n' && src[i] != '\r' {
				out[i] = empty
			}

		case stringLiteral, charLiteral:
			if !keepLiterals {
				out[i] = empty
			}
			quote := byte('"')
			if state == charLiteral {
				quote = '\''
			}
			if src[i] == '\\' && i+1 < len(src) {
				if !keepLiterals {
					out[i+1] = empty
				}
				i++
				continue
			}
			if src[i] == quote {
				if !keepLiterals {
					out[i] = empty
				}
				state = normal
			}

		case rawLiteral:
			if bytes.HasPrefix(src[i:], rawTerm) {
				if !keepLiterals {
					for k := i; k < i+len(rawTerm); k++ {
						out[k] = empty
					}
				}
				i += len(rawTerm) - 1
				state = normal
				rawTerm = nil
			} else if !keepLiterals && src[i] != '\n' && src[i] != '\r' {
				out[i] = empty
			}
		}
	}
	return out
}

func rawOpen(src []byte, at int) (delim []byte, open int, ok bool) {
	start := at + 2
	for i := start; i < len(src) && i < start+17; i++ {
		if src[i] == '(' {
			return src[start:i], i, true
		}
		if src[i] == ' ' || src[i] == ')' || src[i] == '\\' || src[i] == '\t' {
			return nil, 0, false
		}
	}
	return nil, 0, false
}

func classKeywordAt(src []byte, at int) (string, int) {
	if wordAt(src, at, "class") {
		return "class", len("class")
	}
	if wordAt(src, at, "struct") {
		return "struct", len("struct")
	}
	return "", 0
}

func wordAt(src []byte, at int, word string) bool {
	if at < 0 || at+len(word) > len(src) || string(src[at:at+len(word)]) != word {
		return false
	}
	if at > 0 && identByte(src[at-1]) {
		return false
	}
	if at+len(word) < len(src) && identByte(src[at+len(word)]) {
		return false
	}
	return true
}

func precededByEnum(src []byte, at int) bool {
	before := bytes.TrimRight(src[:at], " \t\r\n")
	return len(before) >= 4 && string(before[len(before)-4:]) == "enum" &&
		(len(before) == 4 || !identByte(before[len(before)-5]))
}

func identByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func identStart(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func identifierEnd(src []byte, at int) int {
	for at < len(src) && identByte(src[at]) {
		at++
	}
	return at
}

func skipSpace(src []byte, at int) int {
	for at < len(src) {
		switch src[at] {
		case ' ', '\t', '\r', '\n':
			at++
		default:
			return at
		}
	}
	return at
}

func closingBrace(src []byte, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func closingParen(src []byte, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func normalize(src []byte) []byte {
	return bytes.Join(bytes.Fields(src), []byte(" "))
}

func memberFields(body []byte) []Field {
	var (
		fields     []Field
		segment    = 0
	)
	seen := make(map[string]bool)

	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '{':
			if close := closingBrace(body, i); close >= 0 {
				i = close
				segment = i + 1
			}
		case ':':
			if isAccessLabel(body[segment:i]) {
				segment = i + 1
			}
		case ';':
			for _, field := range parseFieldSegment(body[segment:i]) {
				if !seen[field.Name] {
					seen[field.Name] = true
					fields = append(fields, field)
				}
			}
			segment = i + 1
		}
	}
	return fields
}

func isAccessLabel(seg []byte) bool {
	switch strings.TrimSpace(string(seg)) {
	case "public", "private", "protected":
		return true
	}
	return false
}

func parseFieldSegment(seg []byte) []Field {
	statement := strings.TrimSpace(string(seg))
	if statement == "" || strings.ContainsAny(statement, "()") {
		return nil
	}
	match := fieldHead.FindStringSubmatch(statement)
	if match == nil {
		return nil
	}
	typ := strings.Join(strings.Fields(match[2]), " ")
	decls := strings.Split(match[3], ",")

	var fields []Field
	for _, decl := range decls {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		if cut := strings.IndexByte(decl, '['); cut >= 0 {
			decl = strings.TrimSpace(decl[:cut])
		}
		if cut := strings.IndexByte(decl, ':'); cut >= 0 {
			decl = strings.TrimSpace(decl[:cut])
		}
		value := ""
		if eq := strings.IndexByte(decl, '='); eq >= 0 {
			value = strings.TrimSpace(decl[eq+1:])
			decl = strings.TrimSpace(decl[:eq])
		}
		if decl == "" || !validIdentifier(decl) {
			continue
		}
		field := Field{Type: typ, Name: decl}
		if n, err := strconv.Atoi(value); err == nil {
			field.Default = &n
		}
		fields = append(fields, field)
	}
	return fields
}

func validIdentifier(s string) bool {
	if s == "" || !identStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !identByte(s[i]) {
			return false
		}
	}
	return true
}

func memberMethods(body []byte) []string {
	var methods []string
	seen := make(map[string]bool)

	for i := 0; i < len(body); {
		c := body[i]
		if c == '{' {
			close := closingBrace(body, i)
			if close < 0 {
				break
			}
			i = close + 1
			continue
		}
		if !identStart(c) {
			i++
			continue
		}

		start := i
		for i < len(body) && identByte(body[i]) {
			i++
		}
		token := string(body[start:i])
		open := skipSpace(body, i)
		if open >= len(body) || body[open] != '(' || methodStop[token] {
			continue
		}
		prev := start - 1
		for prev >= 0 && (body[prev] == ' ' || body[prev] == '\t' || body[prev] == '\r' || body[prev] == '\n') {
			prev--
		}
		if prev >= 0 && (body[prev] == '.' || body[prev] == '>' || body[prev] == '~') {
			continue
		}

		close := closingParen(body, open)
		if close < 0 {
			continue
		}
		if !looksLikeSignature(body, close+1) {
			i = close + 1
			continue
		}
		if !seen[token] {
			seen[token] = true
			methods = append(methods, token)
		}
		i = close + 1
	}
	return methods
}

func looksLikeSignature(body []byte, after int) bool {
	i := skipSpace(body, after)
	if i >= len(body) {
		return false
	}
	switch body[i] {
	case '{', ';', ':', '&', '=':
		return true
	case '-':
		return i+1 < len(body) && body[i+1] == '>'
	}
	if !identStart(body[i]) {
		return false
	}
	end := identifierEnd(body, i)
	return methodQualifier[string(body[i:end])]
}
