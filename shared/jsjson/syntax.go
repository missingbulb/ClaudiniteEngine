package jsjson

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

// SyntaxError is the message JSON.parse throws for text, as Node 22's V8
// words it, or "" when text parses: a reader that shows a person why a
// file is not JSON words it as the Node engine did. Positions, lines and
// columns count UTF-16 code units, as JavaScript strings do.
func SyntaxError(text string) string {
	p := &syntaxParser{s: utf16.Encode([]rune(text))}
	p.parse()
	return p.msg
}

// The tokens V8 reads off a value's first character.
const (
	tokEOS = iota
	tokString
	tokNumber
	tokTrue
	tokFalse
	tokNull
	tokWhitespace
	tokColon
	tokComma
	tokLBrack
	tokRBrack
	tokLBrace
	tokRBrace
	tokIllegal
)

// contextChars is V8's kMaxContextCharacters; a source shorter than
// twice it plus one is quoted whole.
const contextChars = 10

type syntaxParser struct {
	s   []uint16
	at  int
	msg string
}

func (p *syntaxParser) failed() bool { return p.msg != "" }

func tokenOf(c uint16) int {
	switch {
	case c == '"':
		return tokString
	case c == '-' || c >= '0' && c <= '9':
		return tokNumber
	case c == 't':
		return tokTrue
	case c == 'f':
		return tokFalse
	case c == 'n':
		return tokNull
	case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		return tokWhitespace
	case c == ':':
		return tokColon
	case c == ',':
		return tokComma
	case c == '[':
		return tokLBrack
	case c == ']':
		return tokRBrack
	case c == '{':
		return tokLBrace
	case c == '}':
		return tokRBrace
	}
	return tokIllegal
}

// peek skips whitespace and is the next token.
func (p *syntaxParser) peek() int {
	for p.at < len(p.s) && tokenOf(p.s[p.at]) == tokWhitespace {
		p.at++
	}
	if p.at >= len(p.s) {
		return tokEOS
	}
	return tokenOf(p.s[p.at])
}

// expect consumes token or fails with message.
func (p *syntaxParser) expect(token int, message string) bool {
	if p.peek() == token {
		p.at++
		return true
	}
	p.fail(p.peek(), message)
	return false
}

func (p *syntaxParser) located(template string) string {
	line, last := 1, 0
	i := 0
	for ; i < p.at; i++ {
		if p.s[i] == '\r' && i < p.at-1 && p.s[i+1] == '\n' {
			i++
		}
		if p.s[i] == '\r' || p.s[i] == '\n' {
			line++
			last = i + 1
		}
	}
	at := " in JSON at position "
	if strings.HasSuffix(template, " after JSON") {
		at = " at position "
	}
	return template + at + strconv.Itoa(p.at) + " (line " + strconv.Itoa(line) + " column " + strconv.Itoa(1+i-last) + ")"
}

func str(units []uint16) string { return string(utf16.Decode(units)) }

// fail records V8's ReportUnexpectedToken: an explicit message names the
// position; otherwise the token picks the message, and a character with
// no message of its own is quoted with its context.
func (p *syntaxParser) fail(token int, message string) {
	if p.failed() {
		return
	}
	if message != "" {
		p.msg = p.located(message)
		return
	}
	switch token {
	case tokEOS:
		p.msg = "Unexpected end of JSON input"
		return
	case tokNumber:
		p.msg = p.located("Unexpected number")
		return
	case tokString:
		p.msg = p.located("Unexpected string")
		return
	}
	whole := str(p.s)
	switch whole {
	case "[object Object]", "undefined", "Infinity", "NaN":
		p.msg = `"` + whole + `" is not valid JSON`
		return
	}
	char := str(p.s[p.at : p.at+1])
	n := len(p.s)
	if n < 2*contextChars+1 {
		p.msg = "Unexpected token '" + char + "', \"" + whole + "\" is not valid JSON"
		return
	}
	switch {
	case p.at < contextChars:
		p.msg = "Unexpected token '" + char + "', \"" + str(p.s[:p.at+contextChars]) + "\"... is not valid JSON"
	case p.at < n-contextChars:
		p.msg = "Unexpected token '" + char + "', ...\"" + str(p.s[p.at-contextChars:p.at+contextChars]) + "\"... is not valid JSON"
	default:
		p.msg = "Unexpected token '" + char + "', ...\"" + str(p.s[p.at-contextChars:]) + "\" is not valid JSON"
	}
}

// failChar is V8's ReportUnexpectedCharacter at the cursor.
func (p *syntaxParser) failChar() {
	if p.at >= len(p.s) {
		p.fail(tokEOS, "")
		return
	}
	c := p.s[p.at]
	if c > 0x7f {
		p.fail(tokIllegal, "")
		return
	}
	p.fail(tokenOf(c), "")
}

func (p *syntaxParser) parse() {
	p.value()
	if p.failed() {
		return
	}
	if p.peek() != tokEOS {
		p.fail(p.peek(), "Unexpected non-whitespace character after JSON")
	}
}

func (p *syntaxParser) value() {
	switch p.peek() {
	case tokString:
		p.at++
		p.scanString()
	case tokNumber:
		p.scanNumber()
	case tokLBrace:
		p.at++
		if p.peek() == tokRBrace {
			p.at++
			return
		}
		first := true
		for {
			msg := "Expected double-quoted property name"
			if first {
				msg = "Expected property name or '}'"
			}
			first = false
			if !p.expect(tokString, msg) {
				return
			}
			if p.scanString(); p.failed() {
				return
			}
			if !p.expect(tokColon, "Expected ':' after property name") {
				return
			}
			if p.value(); p.failed() {
				return
			}
			if p.peek() == tokComma {
				p.at++
				continue
			}
			p.expect(tokRBrace, "Expected ',' or '}' after property value")
			return
		}
	case tokLBrack:
		p.at++
		if p.peek() == tokRBrack {
			p.at++
			return
		}
		for {
			if p.value(); p.failed() {
				return
			}
			if p.peek() == tokComma {
				p.at++
				continue
			}
			p.expect(tokRBrack, "Expected ',' or ']' after array element")
			return
		}
	case tokTrue:
		p.scanLiteral("true")
	case tokFalse:
		p.scanLiteral("false")
	case tokNull:
		p.scanLiteral("null")
	default:
		p.failChar()
	}
}

// scanLiteral reads a literal whose first character matched: the first
// character that differs is the unexpected one.
func (p *syntaxParser) scanLiteral(word string) {
	p.at++
	for i := 1; i < len(word); i++ {
		if p.at >= len(p.s) {
			p.fail(tokEOS, "")
			return
		}
		if p.s[p.at] != uint16(word[i]) {
			p.failChar()
			return
		}
		p.at++
	}
}

func isHex(c uint16) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func isDigit(c uint16) bool { return c >= '0' && c <= '9' }

// scanString reads a string whose opening quote is consumed.
func (p *syntaxParser) scanString() {
	for {
		if p.at >= len(p.s) {
			p.fail(tokIllegal, "Unterminated string")
			return
		}
		c := p.s[p.at]
		switch {
		case c == '"':
			p.at++
			return
		case c < 0x20:
			p.fail(tokIllegal, "Bad control character in string literal")
			return
		case c == '\\':
			p.at++
			if p.at >= len(p.s) {
				p.fail(tokEOS, "")
				return
			}
			e := p.s[p.at]
			switch {
			case e > 0xff:
				p.failChar()
				return
			case e == '"' || e == '\\' || e == '/' || e == 'b' || e == 'f' || e == 'n' || e == 'r' || e == 't':
				p.at++
			case e == 'u':
				for i := 0; i < 4; i++ {
					p.at++
					if p.at >= len(p.s) || !isHex(p.s[p.at]) {
						p.fail(tokIllegal, "Bad Unicode escape")
						return
					}
				}
				p.at++
			default:
				p.fail(tokIllegal, "Bad escaped character")
				return
			}
		default:
			p.at++
		}
	}
}

func (p *syntaxParser) cur() (uint16, bool) {
	if p.at >= len(p.s) {
		return 0, false
	}
	return p.s[p.at], true
}

func (p *syntaxParser) digits() {
	for p.at < len(p.s) && isDigit(p.s[p.at]) {
		p.at++
	}
}

// scanNumber reads a number, failing where V8 does.
func (p *syntaxParser) scanNumber() {
	if c, _ := p.cur(); c == '-' {
		p.at++
	}
	c, ok := p.cur()
	switch {
	case ok && c == '0':
		p.at++
		if c, ok := p.cur(); ok && isDigit(c) {
			p.fail(tokNumber, "")
			return
		}
	case ok && isDigit(c):
		p.digits()
	default:
		p.fail(tokIllegal, "No number after minus sign")
		return
	}
	if c, ok := p.cur(); ok && c == '.' {
		p.at++
		if c, ok := p.cur(); !ok || !isDigit(c) {
			p.fail(tokIllegal, "Unterminated fractional number")
			return
		}
		p.digits()
	}
	if c, ok := p.cur(); ok && (c == 'e' || c == 'E') {
		p.at++
		if c, ok := p.cur(); ok && (c == '-' || c == '+') {
			p.at++
		}
		if c, ok := p.cur(); !ok || !isDigit(c) {
			p.fail(tokIllegal, "Exponent part is missing a number")
			return
		}
		p.digits()
	}
}
