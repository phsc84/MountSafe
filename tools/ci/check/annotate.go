package check

import (
	"fmt"
	"strings"
)

// MaxAnnotations is how many error annotations a job writes at most; GitHub
// shows no more per step (CI-22).
const MaxAnnotations = 10

// MaxAnnotationLines is how many lines of output an annotation carries.
const MaxAnnotationLines = 40

// Annotation is a GitHub error annotation.
type Annotation struct {
	Title   string
	Message string
}

// Annotations renders as as GitHub workflow commands, at most MaxAnnotations
// of them: when there are more, the last one says how many more failed and
// to see the log.
func Annotations(as []Annotation) []string {
	if len(as) > MaxAnnotations {
		more := len(as) - (MaxAnnotations - 1)
		as = append(as[:MaxAnnotations-1:MaxAnnotations-1], Annotation{
			Title:   "more failures",
			Message: fmt.Sprintf("and %d more failures, see the log", more),
		})
	}
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = "::error title=" + escapeProperty(a.Title) + "::" + escapeData(a.Message)
	}
	return out
}

// escapeData escapes the message of a workflow command (CI-23).
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// escapeProperty escapes a property of a workflow command, such as the title.
func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

// FirstLines joins the first n lines of ls.
func FirstLines(ls []string, n int) string {
	if len(ls) > n {
		ls = ls[:n]
	}
	return strings.Join(ls, "\n")
}

// LastLines returns the last n lines of s.
func LastLines(s string, n int) string {
	ls := strings.Split(strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}
