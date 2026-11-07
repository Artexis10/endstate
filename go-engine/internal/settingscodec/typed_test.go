// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package settingscodec

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

const poetryTarget = "${xdg.config}/pypoetry/config.toml"

func TestEmbeddedTypedSpecsAllLoad(t *testing.T) {
	entries, err := fs.ReadDir(typedAssets, "typed")
	if err != nil {
		t.Fatal(err)
	}
	specs := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		specs++
		id := strings.TrimSuffix(entry.Name(), ".jsonc")
		if _, ok := Typed(id); !ok {
			t.Errorf("typed codec spec %s does not load; a broken spec must not look like an unknown codec", entry.Name())
		}
	}
	if specs == 0 || len(TypedIDs()) != specs {
		t.Fatalf("loaded %d typed codecs from %d specs", len(TypedIDs()), specs)
	}
}

func TestTypedPoetryCaptureStoresCanonicalEncodingWithoutCommentsOrOrder(t *testing.T) {
	live := []byte("# note kept by the user: token ghp_comment_secret\r\n" +
		"[virtualenvs.options]\r\nno-pip = true\r\n\r\n" +
		"[virtualenvs]\r\nin-project = true # inline note\r\ncreate = false\r\n\r\n" +
		"[installer]\r\n" + // an empty table carries no setting
		"[keyring]\r\nenabled = false\r\n")
	got, err := Transform("poetry-settings-v1", poetryTarget, live)
	if err != nil {
		t.Fatal(err)
	}
	want := "[keyring]\nenabled = false\n\n[virtualenvs]\ncreate = false\nin-project = true\n\n[virtualenvs.options]\nno-pip = true\n"
	if strings.ReplaceAll(string(got), "\r\n", "\n") != want {
		t.Fatalf("canonical encoding = %q, want %q", got, want)
	}
	if strings.Contains(string(got), "ghp_") {
		t.Fatalf("a comment reached the bundle payload: %q", got)
	}
	again, err := Transform("poetry-settings-v1", poetryTarget, got)
	if err != nil || string(again) != string(got) {
		t.Fatalf("canonical encoding is not stable: %q, %v", again, err)
	}
}

func TestTypedPoetryEmptyDocumentIsAnEmptyValue(t *testing.T) {
	codec, _ := Typed("poetry-settings-v1")
	for name, document := range map[string]string{"empty": "", "comments only": "# nothing\n", "empty table": "[virtualenvs]\n"} {
		t.Run(name, func(t *testing.T) {
			value, err := codec.Decode([]byte(document))
			if err != nil || len(value) != 0 {
				t.Fatalf("decode = %v, %v", value, err)
			}
			encoded, err := codec.Encode(value)
			if err != nil || len(encoded) != 0 {
				t.Fatalf("encode = %q, %v", encoded, err)
			}
		})
	}
}

func TestTypedPoetryRefusesContentOutsideTheSchemaInsteadOfDroppingIt(t *testing.T) {
	for name, content := range map[string]string{
		"unknown top-level key":    "future-option = true\n",
		"unknown nested key":       "[virtualenvs]\nfuture-option = true\n",
		"credential table":         "[http-basic.private-repo]\nusername = \"hugo\"\npassword = \"hunter2\"\n",
		"path outside the schema":  "cache-dir = \"/home/hugo/cache\"\n",
		"deprecated key":           "[virtualenvs]\nprefer-active-python = true\n",
		"string where bool wanted": "[virtualenvs]\ncreate = \"true\"\n",
		"int where bool wanted":    "[virtualenvs]\ncreate = 1\n",
		"bool where int wanted":    "[installer]\nmax-workers = true\n",
		"float where int wanted":   "[installer]\nmax-workers = 4.0\n",
		"int where string wanted":  "[virtualenvs]\nprompt = 4\n",
		"scalar where table":       "virtualenvs = true\n",
		"table where scalar":       "[system-git-client]\nenabled = true\n",
		"array":                    "[virtualenvs]\ncreate = [true]\n",
		"NaN":                      "[installer]\nmax-workers = nan\n",
		"infinity":                 "[installer]\nmax-workers = inf\n",
		"offset datetime":          "[installer]\nmax-workers = 1979-05-27T07:32:00Z\n",
		"local date":               "[virtualenvs]\ncreate = 1979-05-27\n",
		"datetime at unknown key":  "stamp = 1979-05-27T07:32:00Z\n",
		"empty key":                "\"\" = true\n",
		"empty key in a table":     "[virtualenvs]\n\"\" = true\n",
		"NUL in a key":             "\"a\\u0000b\" = true\n",
		"NUL in a string":          "[virtualenvs]\nprompt = \"a\\u0000b\"\n",
		"raw NUL":                  "[virtualenvs]\ncreate = true\x00\n",
		"invalid UTF-8":            "[virtualenvs]\nprompt = \"\xff\"\n",
		"duplicate key":            "[virtualenvs]\ncreate = true\ncreate = false\n",
		"malformed document":       "[virtualenvs\ncreate = true\n",
		"integer beyond int64":     "[installer]\nmax-workers = 9223372036854775808\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Transform("poetry-settings-v1", poetryTarget, []byte(content))
			if err == nil || got != nil {
				t.Fatalf("unsupported content became a usable payload: %q, %v", got, err)
			}
			for _, private := range []string{"private-repo", "hunter2", "hugo", "future-option"} {
				if strings.Contains(err.Error(), private) {
					t.Fatalf("error disclosed document content %q: %v", private, err)
				}
			}
			if err := Validate("poetry-settings-v1", []byte(content)); err == nil {
				t.Fatal("Validate accepted content that Transform refuses")
			}
		})
	}
}

func TestTypedPoetryCodecOwnsOnlyItsOneTarget(t *testing.T) {
	if !Supported("poetry-settings-v1") || !Validates("poetry-settings-v1") {
		t.Fatal("typed codec is not executable and rejecting")
	}
	for _, target := range []string{"${xdg.config}/pypoetry/auth.toml", "${home}/.config/pypoetry/config.toml", "${xdg.config}/pypoetry/config.toml/"} {
		if TargetSupported("poetry-settings-v1", target) {
			t.Fatalf("typed codec claimed %q", target)
		}
		if got, err := Transform("poetry-settings-v1", target, []byte("")); err == nil || got != nil {
			t.Fatalf("typed codec transformed %q", target)
		}
	}
	if !TargetSupported("poetry-settings-v1", poetryTarget) {
		t.Fatal("typed codec does not own its reviewed target")
	}
}

func TestTypedPoetryEqualComparesValuesNotBytes(t *testing.T) {
	codec, _ := Typed("poetry-settings-v1")
	a, err := codec.Decode([]byte("[virtualenvs]\nin-project = true\ncreate = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := codec.Decode([]byte("# reordered\n[virtualenvs]\ncreate = false\nin-project = true\n"))
	if err != nil || !codec.Equal(a, b) {
		t.Fatalf("same value compared unequal: %v, %v", b, err)
	}
	c, _ := codec.Decode([]byte("[virtualenvs]\ncreate = false\nin-project = false\n"))
	if codec.Equal(a, c) {
		t.Fatal("different values compared equal")
	}
	if codec.Equal(a, map[string]any{"future": true}) {
		t.Fatal("a value outside the schema compared equal")
	}
}

func TestTypedPoetryFixturesDecodeToCanonicalFixedPoints(t *testing.T) {
	codec, _ := Typed("poetry-settings-v1")
	fixtures, err := TypedFixtures("poetry-settings-v1")
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("fixtures = %d, %v", len(fixtures), err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			value, err := codec.Decode(fixture.Data)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := codec.Encode(value)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := codec.Decode(encoded)
			if err != nil || !reflect.DeepEqual(decoded, value) {
				t.Fatalf("encode then decode changed the value: %v, %v", decoded, err)
			}
			reencoded, _ := codec.Encode(decoded)
			if string(reencoded) != string(encoded) {
				t.Fatal("canonical encoding is not a fixed point")
			}
		})
	}
}
