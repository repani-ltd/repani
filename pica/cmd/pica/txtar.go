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
	start := 0 // byte offset where the current member's data begins
	close := func(end int) {
		if n := len(files); n > 0 {
			body := data[start:end]
			if body != "" && !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
			files[n-1].data = body
		}
	}
	for pos := 0; pos < len(data); {
		nl := strings.IndexByte(data[pos:], '\n')
		next := len(data)
		if nl >= 0 {
			next = pos + nl + 1
		}
		if name, ok := markerName(strings.TrimSuffix(data[pos:next], "\n")); ok {
			close(pos)
			files = append(files, txtarFile{name: name})
			start = next
		}
		pos = next
	}
	close(len(data))
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
