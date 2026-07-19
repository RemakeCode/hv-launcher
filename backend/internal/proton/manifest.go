package proton

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type vdfValue struct {
	text   string
	object vdfObject
}

type vdfObject map[string][]vdfValue

type vdfParser struct {
	input []rune
	index int
}

func parseProtonManifest(data []byte) (string, error) {
	if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return "", fmt.Errorf("manifest is not valid NUL-free UTF-8")
	}
	parser := vdfParser{input: []rune(string(data))}
	document, err := parser.parseObject(false)
	if err != nil {
		return "", err
	}

	compatibilityTools, ok := objectValue(document, "compatibilitytools")
	if !ok {
		return "", fmt.Errorf("missing compatibilitytools object")
	}
	tools, ok := objectValue(compatibilityTools, "compat_tools")
	if !ok {
		return "", fmt.Errorf("missing compat_tools object")
	}

	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "" {
			continue
		}
		values := tools[name]
		for _, value := range values {
			if value.object == nil {
				continue
			}
			installPath, hasInstallPath := scalarValue(value.object, "install_path")
			if hasInstallPath && installPath == "." {
				return name, nil
			}
		}
	}

	return "", fmt.Errorf("compat_tools does not declare a root-installed compatibility tool")
}

func objectValue(object vdfObject, key string) (vdfObject, bool) {
	for actual, values := range object {
		if !strings.EqualFold(actual, key) {
			continue
		}
		for _, value := range values {
			if value.object != nil {
				return value.object, true
			}
		}
	}
	return nil, false
}

func scalarValue(object vdfObject, key string) (string, bool) {
	for actual, values := range object {
		if !strings.EqualFold(actual, key) {
			continue
		}
		for _, value := range values {
			if value.object == nil {
				return value.text, true
			}
		}
	}
	return "", false
}

func (p *vdfParser) parseObject(requireClose bool) (vdfObject, error) {
	result := make(vdfObject)
	for {
		p.skipSpaceAndComments()
		if p.index >= len(p.input) {
			if requireClose {
				return nil, fmt.Errorf("object is missing a closing brace")
			}
			return result, nil
		}
		if p.input[p.index] == '}' {
			if !requireClose {
				return nil, fmt.Errorf("unexpected closing brace")
			}
			p.index++
			return result, nil
		}

		key, err := p.parseToken()
		if err != nil {
			return nil, err
		}
		p.skipSpaceAndComments()
		if p.index >= len(p.input) {
			return nil, fmt.Errorf("key %q has no value", key)
		}

		var value vdfValue
		if p.input[p.index] == '{' {
			p.index++
			value.object, err = p.parseObject(true)
		} else {
			value.text, err = p.parseToken()
		}
		if err != nil {
			return nil, err
		}
		result[key] = append(result[key], value)
	}
}

func (p *vdfParser) parseToken() (string, error) {
	p.skipSpaceAndComments()
	if p.index >= len(p.input) {
		return "", fmt.Errorf("expected token")
	}
	if p.input[p.index] == '{' || p.input[p.index] == '}' {
		return "", fmt.Errorf("expected token before brace")
	}
	if p.input[p.index] != '"' {
		start := p.index
		for p.index < len(p.input) && !unicode.IsSpace(p.input[p.index]) && p.input[p.index] != '{' && p.input[p.index] != '}' {
			p.index++
		}
		if start == p.index {
			return "", fmt.Errorf("expected token")
		}
		return string(p.input[start:p.index]), nil
	}

	p.index++
	var value strings.Builder
	for p.index < len(p.input) {
		current := p.input[p.index]
		p.index++
		switch current {
		case '"':
			return value.String(), nil
		case '\\':
			if p.index >= len(p.input) {
				return "", fmt.Errorf("unterminated escape sequence")
			}
			escaped := p.input[p.index]
			p.index++
			switch escaped {
			case 'n':
				value.WriteRune('\n')
			case 't':
				value.WriteRune('\t')
			default:
				value.WriteRune(escaped)
			}
		default:
			value.WriteRune(current)
		}
	}
	return "", fmt.Errorf("unterminated quoted token")
}

func (p *vdfParser) skipSpaceAndComments() {
	for p.index < len(p.input) {
		if unicode.IsSpace(p.input[p.index]) {
			p.index++
			continue
		}
		if p.input[p.index] == '/' && p.index+1 < len(p.input) && p.input[p.index+1] == '/' {
			p.index += 2
			for p.index < len(p.input) && p.input[p.index] != '\n' {
				p.index++
			}
			continue
		}
		return
	}
}
