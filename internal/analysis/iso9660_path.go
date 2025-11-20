package analysis

import (
	"path"
	"runtime"
	"strings"
)

const (
	iso9660DirectoryIdentifierMaxLength = 31
	iso9660FileIdentifierMaxLength      = 30
)

// iso9660Characters defines the allowed characters for ISO9660 D-strings.
// This matches the character set used by github.com/kdomanski/iso9660.
const iso9660Characters = "abcdefghijklmnopqrstuvwxyz0123456789_!\"%&'()*+,-./:;<=>?"

// iso9660RelativePath converts a host-relative path into the mangled path that
// the ISO9660 writer produces so the guest can address the file correctly.
func iso9660RelativePath(rel string) string {
	clean := iso9660SplitPath(rel)
	if len(clean) == 0 {
		return ""
	}

	segments := make([]string, len(clean))
	for i, segment := range clean {
		if i == len(clean)-1 {
			name := iso9660MangleFileName(segment)
			segments[i] = strings.TrimSuffix(name, ";1")
			continue
		}
		segments[i] = iso9660MangleDirectoryName(segment)
	}
	return path.Join(segments...)
}

func iso9660SplitPath(p string) []string {
	if runtime.GOOS == "windows" {
		p = strings.ReplaceAll(p, "\\", "/")
	}
	raw := strings.Split(p, "/")
	out := make([]string, 0, len(raw))
	for _, segment := range raw {
		if segment == "" {
			continue
		}
		out = append(out, segment)
	}
	return out
}

func iso9660MangleDirectoryName(input string) string {
	return iso9660MangleDString(input, iso9660DirectoryIdentifierMaxLength)
}

func iso9660MangleFileName(input string) string {
	input = strings.ToLower(input)
	parts := strings.Split(input, ".")

	version := "1"
	filename := parts[0]
	extension := ""
	if len(parts) > 1 {
		filename = strings.Join(parts[:len(parts)-1], "_")
		extension = parts[len(parts)-1]
	}

	extension = iso9660MangleDString(extension, 8)

	maxFilenameLen := iso9660FileIdentifierMaxLength - (1 + len(version))
	if extension != "" {
		maxFilenameLen -= (1 + len(extension))
	}

	filename = iso9660MangleDString(filename, maxFilenameLen)

	if extension != "" {
		return filename + "." + extension + ";" + version
	}
	return filename + ";" + version
}

func iso9660MangleDString(input string, maxLen int) string {
	input = strings.ToLower(input)
	var b strings.Builder
	for i := 0; i < len(input) && b.Len() < maxLen; i++ {
		c := rune(input[i])
		if strings.ContainsRune(iso9660Characters, c) {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
