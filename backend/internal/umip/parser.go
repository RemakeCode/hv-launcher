package umip

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	grubVariable   = "GRUB_CMDLINE_LINUX_DEFAULT"
	limineVariable = "KERNEL_CMDLINE[default]"
)

type assignment struct {
	operator string
	value    string
}

func parseGRUB(data []byte) ([]string, error) {
	assignments := make([]assignment, 0, 1)
	for lineNumber, line := range strings.Split(string(data), "\n") {
		parsed, matched, err := parseAssignment(line, grubVariable, false, true)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
		if matched {
			assignments = append(assignments, parsed)
		}
	}
	if len(assignments) == 0 {
		return nil, fmt.Errorf("%s is missing", grubVariable)
	}
	if len(assignments) != 1 {
		return nil, fmt.Errorf("%s must have exactly one active assignment", grubVariable)
	}
	return []string{assignments[0].value}, nil
}

func parseLimine(data []byte) ([]string, error) {
	values := make([]string, 0, 2)
	for lineNumber, line := range strings.Split(string(data), "\n") {
		parsed, matched, err := parseAssignment(line, limineVariable, true, false)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
		if !matched {
			continue
		}
		if parsed.operator == "=" {
			values = values[:0]
		}
		values = append(values, parsed.value)
	}
	return values, nil
}

func parseAssignment(line, variable string, allowAppend, requireQuoted bool) (assignment, bool, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return assignment{}, false, nil
	}
	for _, prefix := range []string{"export ", "readonly "} {
		if strings.HasPrefix(trimmed, prefix+variable) {
			return assignment{}, true, fmt.Errorf("%s uses unsupported shell syntax", variable)
		}
	}
	if !strings.HasPrefix(trimmed, variable) {
		return assignment{}, false, nil
	}
	remainder := trimmed[len(variable):]
	if remainder != "" && isVariableContinuation(rune(remainder[0])) {
		return assignment{}, false, nil
	}
	remainder = strings.TrimLeftFunc(remainder, unicode.IsSpace)
	operator := ""
	switch {
	case strings.HasPrefix(remainder, "+="):
		if !allowAppend {
			return assignment{}, true, fmt.Errorf("%s append assignments are not supported", variable)
		}
		operator = "+="
		remainder = remainder[2:]
	case strings.HasPrefix(remainder, "="):
		operator = "="
		remainder = remainder[1:]
	default:
		return assignment{}, true, fmt.Errorf("%s is not a supported assignment", variable)
	}
	value, err := parseStaticValue(remainder, requireQuoted)
	if err != nil {
		return assignment{}, true, fmt.Errorf("invalid %s value: %w", variable, err)
	}
	return assignment{operator: operator, value: value}, true, nil
}

func parseStaticValue(raw string, requireQuoted bool) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("value is empty")
	}
	if value[0] == '\'' || value[0] == '"' {
		quote := value[0]
		closing := closingQuote(value, quote)
		if closing < 0 {
			return "", fmt.Errorf("quoted value is incomplete")
		}
		remainder := value[closing+1:]
		trimmedRemainder := strings.TrimSpace(remainder)
		if trimmedRemainder != "" {
			withoutLeadingSpace := strings.TrimLeftFunc(remainder, unicode.IsSpace)
			if len(withoutLeadingSpace) == len(remainder) || !strings.HasPrefix(trimmedRemainder, "#") {
				return "", fmt.Errorf("quoted value has trailing shell syntax")
			}
		}
		return value[1:closing], nil
	}
	if requireQuoted {
		return "", fmt.Errorf("value must use complete single or double quotes")
	}
	if comment := unquotedComment(value); comment >= 0 {
		value = strings.TrimSpace(value[:comment])
	}
	if value == "" || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("unquoted value must be one static token")
	}
	if strings.ContainsAny(value, "'\";|&`(){}<>") {
		return "", fmt.Errorf("unquoted value contains unsupported shell syntax")
	}
	return value, nil
}

func closingQuote(value string, quote byte) int {
	escaped := false
	for index := 1; index < len(value); index++ {
		character := value[index]
		if quote == '"' && character == '\\' && !escaped {
			escaped = true
			continue
		}
		if character == quote && !escaped {
			return index
		}
		escaped = false
	}
	return -1
}

func unquotedComment(value string) int {
	for index, character := range value {
		if character == '#' && (index == 0 || unicode.IsSpace(rune(value[index-1]))) {
			return index
		}
	}
	return -1
}

func isVariableContinuation(character rune) bool {
	return character == '_' || character == '[' || unicode.IsLetter(character) || unicode.IsDigit(character)
}

func inspectArguments(values []string) (CandidateState, string) {
	configured := ""
	for _, value := range values {
		for _, token := range strings.Fields(value) {
			switch token {
			case "clearcpuid=514", "clearcpuid=umip":
				if configured == "" {
					configured = token
				}
			default:
				if token == "clearcpuid" || strings.HasPrefix(token, "clearcpuid=") {
					return "", token
				}
			}
		}
	}
	if configured != "" {
		return StateConfigured, configured
	}
	return StateActionRequired, ""
}
