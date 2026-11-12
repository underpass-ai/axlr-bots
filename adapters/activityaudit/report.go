package activityaudit

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

// reportError bounds the error column of the report, in characters.
const reportError = 90

// Report prints the signatures as a table for a person: published or
// not, how often and by how many bots, what failed and how, oldest
// first.
func Report(w io.Writer, signatures []SeenSignature) error {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tPUBLISHED\tCOUNT\tRECOVERED\tBOTS\tWHAT\tERROR\tFIRST")
	for _, s := range signatures {
		published := "no"
		if s.PublishedAt != "" {
			published = s.PublishedAt[:min(len(s.PublishedAt), 16)]
		}
		fmt.Fprintf(table, "%s\t%s\t%d\t%d\t%s\t%s\t%s\t%s\n", s.ID, published, s.Count, s.Recovered, bots(s.Bots), What(s), cutRunes(s.Error, reportError), s.FirstAt[:min(len(s.FirstAt), 16)])
	}
	return table.Flush()
}

// What names what failed: the task, or the tool with its program or its
// plugin's tool; a partial signature says so.
func What(s SeenSignature) string {
	if s.Kind == KindTaskFailed {
		return "task failed"
	}
	what := s.Tool
	switch {
	case s.Program != "":
		what += " " + s.Program
	case s.McpTool != "":
		what += " " + s.McpTool
	}
	if s.Partial {
		what += " (partial)"
	}
	return what
}

// bots is the list of bots, the first three named.
func bots(list []string) string {
	if len(list) <= 3 {
		return strings.Join(list, ",")
	}
	return strings.Join(list[:3], ",") + ",+" + strconv.Itoa(len(list)-3)
}
