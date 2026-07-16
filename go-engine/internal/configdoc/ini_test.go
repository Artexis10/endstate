// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configdoc

import (
	"reflect"
	"testing"
)

func TestParseAndEncodeINIDiscardsCommentsAndSortsExactCase(t *testing.T) {
	input := []byte("; first comment\r\nz=3\r\nA=global\r\n\r\n[beta]\r\nz=last\r\nA=first\r\n# second comment\r\n[Alpha]\r\nkey=value=with=equals\r\n[alpha]\r\nKey=upper\r\nkey=lower\r\n")
	document, err := ParseINI(input)
	if err != nil {
		t.Fatal(err)
	}
	encoded := EncodeINI(document)
	want := "A=global\nz=3\n\n[Alpha]\nkey=value=with=equals\n\n[alpha]\nKey=upper\nkey=lower\n\n[beta]\nA=first\nz=last\n"
	if string(encoded) != want {
		t.Fatalf("EncodeINI =\n%s\nwant:\n%s", encoded, want)
	}
}

func TestINIEditPreservesWhitespaceInUnrelatedValues(t *testing.T) {
	document, err := ParseINI([]byte("[section]\nchange=old\nuntouched=  keep me  \n"))
	if err != nil {
		t.Fatal(err)
	}
	document, err = INISet(document, "section", "change", "new")
	if err != nil {
		t.Fatal(err)
	}
	want := "[section]\nchange=new\nuntouched=  keep me  \n"
	if got := string(EncodeINI(document)); got != want {
		t.Fatalf("EncodeINI = %q, want %q", got, want)
	}
}

func TestParseINIRejectsMalformedDocumentsAndDuplicates(t *testing.T) {
	invalid := [][]byte{
		append([]byte("key="), 0xff),
		[]byte("key:value\n"),
		[]byte("=value\n"),
		[]byte("[]\n"),
		[]byte("[section] trailing\n"),
		[]byte("[section\n"),
		[]byte("[section]\nkey=one\nkey=two\n"),
		[]byte("[section]\nkey=one\n[section]\nother=two\n"),
		[]byte("key=value\x00hidden\n"),
	}
	for _, data := range invalid {
		if _, err := ParseINI(data); CodeOf(err) != CodeMalformedINI {
			t.Errorf("ParseINI(%q) error = %v, code = %q", data, err, CodeOf(err))
		}
	}
}

func TestINIEditsCreateSetDeleteAndMoveKeys(t *testing.T) {
	document, err := ParseINI([]byte("[source]\nkey=value\nremove=gone\n[target]\nexisting=keep\n"))
	if err != nil {
		t.Fatal(err)
	}
	document, err = INISet(document, "created", "new", "set")
	if err != nil {
		t.Fatal(err)
	}
	document, err = INIMove(document, "source", "key", "target", "moved")
	if err != nil {
		t.Fatal(err)
	}
	document, err = INIDelete(document, "source", "remove")
	if err != nil {
		t.Fatal(err)
	}

	want := "[created]\nnew=set\n\n[source]\n\n[target]\nexisting=keep\nmoved=value\n"
	if got := string(EncodeINI(document)); got != want {
		t.Fatalf("EncodeINI =\n%s\nwant:\n%s", got, want)
	}
}

func TestINISetRejectsNonCanonicalStringValues(t *testing.T) {
	document, err := ParseINI([]byte("[section]\nkey=value\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"line one\nline two", "line one\rline two", "value\x00hidden", string([]byte{0xff})} {
		if _, err := INISet(document, "section", "key", value); CodeOf(err) != CodeInvalidINIValue {
			t.Errorf("INISet value %q error = %v, code = %q", value, err, CodeOf(err))
		}
	}
}

func TestINIFailuresDoNotMutateInputDocument(t *testing.T) {
	document, err := ParseINI([]byte("[source]\nkey=value\n[target]\nexisting=keep\n"))
	if err != nil {
		t.Fatal(err)
	}
	original := EncodeINI(document)
	tests := []struct {
		name string
		code Code
		edit func(*INI) (*INI, error)
	}{
		{"delete missing source", CodeINISourceMissing, func(doc *INI) (*INI, error) {
			return INIDelete(doc, "source", "missing")
		}},
		{"move missing source", CodeINISourceMissing, func(doc *INI) (*INI, error) {
			return INIMove(doc, "missing", "key", "target", "created")
		}},
		{"move destination exists", CodeINIDestinationExists, func(doc *INI) (*INI, error) {
			return INIMove(doc, "source", "key", "target", "existing")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.edit(document); CodeOf(err) != tt.code {
				t.Fatalf("edit error = %v, code = %q, want %q", err, CodeOf(err), tt.code)
			}
			if after := EncodeINI(document); !reflect.DeepEqual(after, original) {
				t.Fatalf("input mutated:\n%s\nwant:\n%s", after, original)
			}
		})
	}
}
