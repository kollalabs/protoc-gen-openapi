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

func TestResourceNamePattern(t *testing.T) {
	tests := []struct {
		pattern   string
		idPattern string
		want      string
	}{
		{"shelves/{shelf}/books/{book}", "[a-z2-7]{26}", "^shelves/[a-z2-7]{26}/books/[a-z2-7]{26}$"},
		{"projects/{project}/v1.0/{item}", "[a-z2-7]{26}", `^projects/[a-z2-7]{26}/v1\.0/[a-z2-7]{26}$`},
		{"settings", "[a-z2-7]{26}", "^settings$"},
		{"shelves/{shelf}", "[0-9]+|[a-z]{26}", "^shelves/(?:[0-9]+|[a-z]{26})$"},
	}
	for _, tt := range tests {
		if got := resourceNamePattern(tt.pattern, tt.idPattern); got != tt.want {
			t.Errorf("resourceNamePattern(%q, %q) = %q, want %q", tt.pattern, tt.idPattern, got, tt.want)
		}
	}
}
