// Copyright 2020 Google LLC. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package generator

import (
	"regexp"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// contains returns true if an array contains a specified string.
func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// appendUnique appends a string, to a string slice, if the string is not already in the slice
func appendUnique(s []string, e string) []string {
	if !contains(s, e) {
		return append(s, e)
	}
	return s
}

// singular produces the singular form of a collection name.
func singular(plural string) string {
	if strings.HasSuffix(plural, "ves") {
		return strings.TrimSuffix(plural, "ves") + "f"
	}
	if strings.HasSuffix(plural, "ies") {
		return strings.TrimSuffix(plural, "ies") + "y"
	}
	if strings.HasSuffix(plural, "s") {
		return strings.TrimSuffix(plural, "s")
	}
	return plural
}

// filterCommentStringForSummary prepares comment (or method name if there is no comment) for summary value on methods
func (g *OpenAPIv3Generator) filterCommentStringForSummary(c protogen.Comments, goName string) string {
	r := regexp.MustCompile(`\n|,|\.`)

	comment := string(c)
	split := r.Split(comment, 2)
	comment = g.linterRulePattern.ReplaceAllString(split[0], "")
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = strings.TrimSpace(goName)
	}
	return comment
}
