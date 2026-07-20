package cpuidmodule

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var expectedDKMSAssignments = map[string]string{
	"PACKAGE_NAME":         "cpuid_fault_emulation",
	"BUILT_MODULE_NAME":    "cpuid_fault_emulation",
	"DEST_MODULE_LOCATION": "/updates",
	"AUTOINSTALL":          "yes",
}

var dkmsVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~-]{0,63}$`)

func ParseDKMSConfig(data []byte) (Identity, error) {
	values := make(map[string]string, len(expectedDKMSAssignments))
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024), int(MaxDKMSConfigBytes))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, err := parseStaticAssignment(line)
		if err != nil {
			return Identity{}, fmt.Errorf("%w at line %d: %v", ErrInvalidDKMSConfig, lineNumber, err)
		}
		expected, fixed := expectedDKMSAssignments[name]
		if !fixed && name != "PACKAGE_VERSION" {
			return Identity{}, fmt.Errorf("%w at line %d: unsupported assignment %s", ErrInvalidDKMSConfig, lineNumber, name)
		}
		if fixed && value != expected {
			return Identity{}, fmt.Errorf("%w at line %d: %s must be %q", ErrInvalidDKMSConfig, lineNumber, name, expected)
		}
		if name == "PACKAGE_VERSION" && !dkmsVersionPattern.MatchString(value) {
			return Identity{}, fmt.Errorf("%w at line %d: PACKAGE_VERSION is not a safe DKMS version", ErrInvalidDKMSConfig, lineNumber)
		}
		if previous, exists := values[name]; exists && previous != value {
			return Identity{}, fmt.Errorf("%w at line %d: conflicting %s assignment", ErrInvalidDKMSConfig, lineNumber, name)
		}
		values[name] = value
	}
	if err := scanner.Err(); err != nil {
		return Identity{}, fmt.Errorf("%w: read static assignments: %v", ErrInvalidDKMSConfig, err)
	}
	for name := range expectedDKMSAssignments {
		if _, present := values[name]; !present {
			return Identity{}, fmt.Errorf("%w: required assignment %s is missing", ErrInvalidDKMSConfig, name)
		}
	}
	if _, present := values["PACKAGE_VERSION"]; !present {
		return Identity{}, fmt.Errorf("%w: required assignment PACKAGE_VERSION is missing", ErrInvalidDKMSConfig)
	}
	return Identity{
		PackageName: values["PACKAGE_NAME"], PackageVersion: values["PACKAGE_VERSION"],
		BuiltModuleName: values["BUILT_MODULE_NAME"], Destination: values["DEST_MODULE_LOCATION"],
		AutomaticInstall: values["AUTOINSTALL"] == "yes",
	}, nil
}

func parseStaticAssignment(line string) (string, string, error) {
	equals := strings.IndexByte(line, '=')
	if equals <= 0 {
		return "", "", fmt.Errorf("only static assignments are allowed")
	}
	name := strings.TrimSpace(line[:equals])
	if name == "" || strings.IndexFunc(name, func(character rune) bool {
		return character != '_' && !unicode.IsUpper(character)
	}) >= 0 {
		return "", "", fmt.Errorf("invalid assignment name")
	}
	raw := strings.TrimSpace(line[equals+1:])
	if raw == "" {
		return "", "", fmt.Errorf("%s has an empty value", name)
	}
	value, remainder, err := staticValue(raw)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", name, err)
	}
	if strings.TrimSpace(remainder) != "" {
		trimmed := strings.TrimSpace(remainder)
		if len(strings.TrimLeftFunc(remainder, unicode.IsSpace)) == len(remainder) || !strings.HasPrefix(trimmed, "#") {
			return "", "", fmt.Errorf("%s contains executable or trailing shell syntax", name)
		}
	}
	return name, value, nil
}

func staticValue(raw string) (string, string, error) {
	if raw[0] == '\'' || raw[0] == '"' {
		quote := raw[0]
		for index := 1; index < len(raw); index++ {
			if raw[index] == '\\' || raw[index] == '$' || raw[index] == '`' {
				return "", "", fmt.Errorf("escapes and expansion are not allowed")
			}
			if raw[index] == quote {
				return raw[1:index], raw[index+1:], nil
			}
		}
		return "", "", fmt.Errorf("quoted value is incomplete")
	}
	end := len(raw)
	for index, character := range raw {
		if unicode.IsSpace(character) {
			end = index
			break
		}
	}
	value := raw[:end]
	if strings.ContainsAny(value, "'\"$`;|&(){}<>\\") {
		return "", "", fmt.Errorf("shell syntax is not allowed")
	}
	return value, raw[end:], nil
}
