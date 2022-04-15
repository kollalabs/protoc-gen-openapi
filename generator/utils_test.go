package generator

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
)

func TestSummary(t *testing.T) {
	g := NewOpenAPIv3Generator(nil, Configuration{})
	var comments protogen.Comments = `This function updates a message.
 (-- api-linter: core::0xxx::xxx=disabled
     aip.dev/not-precedent: We need to do this because reasons. --)
	`
	filtered := g.linterRulePattern.ReplaceAllString(string(comments), "")
	if strings.Contains(filtered, "0xxx") {
		t.Fatalf("linter rule pattern did not remove linter message\n %s\n", filtered)
	}

}
