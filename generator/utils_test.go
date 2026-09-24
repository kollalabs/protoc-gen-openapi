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
		pattern string
		want    string
	}{
		{"shelves/{shelf}/books/{book}", "^shelves/[a-z2-7]{26}/books/[a-z2-7]{26}$"},
		{"projects/{project}/v1.0/{item}", `^projects/[a-z2-7]{26}/v1\.0/[a-z2-7]{26}$`},
		{"settings", "^settings$"},
	}
	for _, tt := range tests {
		if got := resourceNamePattern(tt.pattern, "[a-z2-7]{26}"); got != tt.want {
			t.Errorf("resourceNamePattern(%q) = %q, want %q", tt.pattern, got, tt.want)
		}
	}
}
