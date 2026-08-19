// Minimal txtar parser -- the subset of golang.org/x/tools/txtar that
// pica needs, kept here so the module carries no dependency for a
// format that is three lines of grammar. A txtar archive is a
// sequence of members, each introduced by a marker line "-- NAME --";
// content runs to the next marker. Text before the first marker (the
// archive comment) is ignored.
package main

import "strings"

type txtarFile struct {
	name string
	data string
}

// parseArchive splits a txtar archive into its members. Member data
// always ends in a newline (one is added when the archive's final
// line lacks its own); pica trims per its own conventions
// afterwards.
func parseArchive(data string) []txtarFile {
	var files []txtarFile
	for len(data) > 0 {
		line, rest, _ := strings.Cut(data, "\n")
		data = rest
		if name, ok := markerName(line); ok {
			files = append(files, txtarFile{name: name})
		} else if n := len(files); n > 0 {
			files[n-1].data += line + "\n"
		}
	}
	return files
}

// markerName reports whether line is a txtar member marker
// ("-- NAME --") and returns the trimmed name.
func markerName(line string) (string, bool) {
	line = strings.TrimSuffix(line, "\r")
	if !strings.HasPrefix(line, "-- ") || !strings.HasSuffix(line, " --") {
		return "", false
	}
	name := strings.TrimSpace(line[3 : len(line)-3])
	return name, name != ""
}
