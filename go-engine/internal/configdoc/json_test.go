// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configdoc

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestValidateJSONPathAcceptsOnlyDocumentedSubset(t *testing.T) {
	valid := []string{
		"$",
		"$.field",
		"$._field2",
		`$["quoted-key"]`,
		`$.array[0]["quoted-key"]`,
		`$["escaped\"key"]`,
	}
	for _, path := range valid {
		if err := ValidateJSONPath(path); err != nil {
			t.Errorf("ValidateJSONPath(%q): %v", path, err)
		}
	}

	invalid := []string{
		"", "field", "$.", "$.hyphen-key", "$..field", "$.*",
		"$[ * ]", "$[*]", "$[?(@.x)]", "$[0:2]", "$[-1]",
		"$[01]", "$[(script)]", "$/regex/", `$['single-quoted']`,
		`$["unterminated]`, "$.field trailing",
	}
	for _, path := range invalid {
		if err := ValidateJSONPath(path); CodeOf(err) != CodeInvalidJSONPath {
			t.Errorf("ValidateJSONPath(%q) error = %v, code = %q", path, err, CodeOf(err))
		}
	}
}

func TestJSONPathExistsUsesDocumentedSubset(t *testing.T) {
	document, err := ParseJSON([]byte(`{"object":{"array":[{"quoted-key":true}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"$", `$.object.array[0]["quoted-key"]`} {
		exists, err := JSONPathExists(document, path)
		if err != nil || !exists {
			t.Errorf("JSONPathExists(%q) = %v, %v; want true, nil", path, exists, err)
		}
	}
	exists, err := JSONPathExists(document, "$.object.missing")
	if err != nil || exists {
		t.Fatalf("missing JSONPathExists = %v, %v; want false, nil", exists, err)
	}
	if _, err := JSONPathExists(document, "$[*]"); CodeOf(err) != CodeInvalidJSONPath {
		t.Fatalf("invalid JSONPathExists error = %v, code = %q", err, CodeOf(err))
	}
}

func TestJSONSetDeepCopiesValueAndPreservesNumbers(t *testing.T) {
	document, err := ParseJSON([]byte(`{"large":900719925474099312345,"settings":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]any{"nested": []any{"original"}}
	edited, err := JSONSet(document, "$.settings.value", value)
	if err != nil {
		t.Fatal(err)
	}
	value["nested"].([]any)[0] = "mutated"

	encoded, err := EncodeJSON(edited)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"large\": 900719925474099312345,\n  \"settings\": {\n    \"value\": {\n      \"nested\": [\n        \"original\"\n      ]\n    }\n  }\n}\n"
	if string(encoded) != want {
		t.Fatalf("EncodeJSON =\n%s\nwant:\n%s", encoded, want)
	}
}

func TestJSONEditsSupportObjectsAndArrays(t *testing.T) {
	document, err := ParseJSON([]byte(`{"array":[1,2,3],"source":{"value":"move"},"target":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	document, err = JSONSet(document, "$.array[1]", json.Number("20"))
	if err != nil {
		t.Fatal(err)
	}
	document, err = JSONDelete(document, "$.array[0]")
	if err != nil {
		t.Fatal(err)
	}
	document, err = JSONMove(document, "$.source.value", "$.target.moved")
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := EncodeJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"array\": [\n    20,\n    3\n  ],\n  \"source\": {},\n  \"target\": {\n    \"moved\": \"move\"\n  }\n}\n"
	if string(encoded) != want {
		t.Fatalf("EncodeJSON =\n%s\nwant:\n%s", encoded, want)
	}
}

func TestParseJSONRejectsMalformedMultipleAndInvalidUTF8(t *testing.T) {
	invalid := [][]byte{
		{},
		[]byte(`{"a":`),
		[]byte(`{"a":1} {"b":2}`),
		append([]byte(`{"a":"`), 0xff, '"', '}'),
	}
	for _, data := range invalid {
		if _, err := ParseJSON(data); CodeOf(err) != CodeMalformedJSON {
			t.Errorf("ParseJSON(%q) error = %v, code = %q", data, err, CodeOf(err))
		}
	}
	if _, err := ParseJSON([]byte("{\"a\":1}\r\n\t ")); err != nil {
		t.Fatalf("trailing whitespace rejected: %v", err)
	}
}

func TestJSONEditFailuresDoNotMutateInputDocument(t *testing.T) {
	document, err := ParseJSON([]byte(`{"array":[1],"object":{"existing":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	original, err := EncodeJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		code Code
		edit func(any) (any, error)
	}{
		{"set missing parent", CodeJSONParentMissing, func(doc any) (any, error) {
			return JSONSet(doc, "$.missing.value", true)
		}},
		{"set missing array index", CodeJSONParentMissing, func(doc any) (any, error) {
			return JSONSet(doc, "$.array[1]", true)
		}},
		{"delete missing source", CodeJSONSourceMissing, func(doc any) (any, error) {
			return JSONDelete(doc, "$.object.missing")
		}},
		{"move missing source", CodeJSONSourceMissing, func(doc any) (any, error) {
			return JSONMove(doc, "$.missing", "$.object.created")
		}},
		{"move destination exists", CodeJSONDestinationExists, func(doc any) (any, error) {
			return JSONMove(doc, "$.array[0]", "$.object.existing")
		}},
		{"move destination array slot exists", CodeJSONDestinationExists, func(doc any) (any, error) {
			return JSONMove(doc, "$.object.existing", "$.array[0]")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.edit(document); CodeOf(err) != tt.code {
				t.Fatalf("edit error = %v, code = %q, want %q", err, CodeOf(err), tt.code)
			}
			after, err := EncodeJSON(document)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, original) {
				t.Fatalf("input mutated:\n%s\nwant:\n%s", after, original)
			}
		})
	}
}
