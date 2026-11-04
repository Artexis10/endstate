// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package registryfile_test

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
)

// utf16Export renders export text the way reg.exe writes it: UTF-16LE with a
// byte-order mark and CRLF line endings.
func utf16Export(lines ...string) []byte {
	units := utf16.Encode([]rune(strings.Join(lines, "\r\n") + "\r\n"))
	data := make([]byte, 2+len(units)*2)
	data[0], data[1] = 0xff, 0xfe
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[2+index*2:], unit)
	}
	return data
}

func decodeUTF16Export(t *testing.T, data []byte) string {
	t.Helper()
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xfe || len(data)%2 != 0 {
		t.Fatalf("filtered export is not UTF-16LE with a byte-order mark")
	}
	units := make([]uint16, (len(data)-2)/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[2+index*2:])
	}
	return string(utf16.Decode(units))
}

func exampleSecretScope(t *testing.T) modules.RegistrySecretScope {
	t.Helper()
	scope, conflicts := modules.ResolveRegistrySecretScope(&modules.Module{
		ID: "apps.example",
		Secrets: &modules.SecretsDef{
			Files: []string{`HKCU\Software\Vendor\App\Name`},
			RegistryValues: []modules.RegistrySecretValue{
				{Key: `HKCU\Software\Vendor\App`, ValueName: "Password", Subkeys: true},
				{Key: `HKCU\Software\Vendor\App\Blank`},
			},
		},
		Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: `HKCU\Software\Vendor\App`}}},
	})
	if len(conflicts) != 0 {
		t.Fatalf("example scope conflicts: %+v", conflicts)
	}
	return scope
}

// TestFilterSecretsRemovesSecretMaterialKeepsSettings catches the two ways a
// filtered export goes wrong: a declared secret survives (in any spelling or
// layout reg.exe produces), or an ordinary setting next to it is damaged or
// dropped. Every SECRET sentinel below sits where the declarations put it.
func TestFilterSecretsRemovesSecretMaterialKeepsSettings(t *testing.T) {
	export := utf16Export(
		"Windows Registry Editor Version 5.00",
		"",
		`[HKEY_CURRENT_USER\Software\Vendor\App]`,
		`@="default-setting"`,
		`"NAME"="SECRET-name-value"`,
		`"Say \"hi\""="kept"`,
		`"Password"="SECRET-top-password"`,
		`"Notes"="line one`,
		`line two"`,
		`"Blob"=hex:01,02,\`,
		`  03`,
		"",
		`[HKEY_CURRENT_USER\Software\Vendor\App\Connections\office]`,
		`"password"=hex(3):53,45,43,52,45,54,\`,
		`  2d,68,65,78`,
		`"Host"="viewer.example.invalid"`,
		"",
		`[HKEY_CURRENT_USER\Software\Vendor\App\Name]`,
		`"Anything"="SECRET-subtree"`,
		"",
		`[HKEY_CURRENT_USER\Software\Vendor\App\Name\Deeper]`,
		`"More"="SECRET-deeper"`,
		"",
		`[HKEY_CURRENT_USER\Software\Vendor\App\Blank]`,
		`@="SECRET-default"`,
		`"Other"="SECRET-other"`,
		"",
		`[HKEY_CURRENT_USER\Software\Vendor\App\Odd: 100% ~$]`,
		`"Mode"=dword:00000001`,
	)

	filtered, report, err := registryfile.FilterSecrets(export, `HKCU\Software\Vendor\App`, exampleSecretScope(t))
	if err != nil {
		t.Fatalf("FilterSecrets() error = %v", err)
	}
	text := decodeUTF16Export(t, filtered)
	for _, secret := range []string{"SECRET", "53,45,43,52,45,54", "2d,68,65,78", `\Name]`, `\Name\Deeper]`} {
		if strings.Contains(text, secret) {
			t.Errorf("filtered export still contains %q:\n%s", secret, text)
		}
	}
	for _, kept := range []string{
		"Windows Registry Editor Version 5.00\r\n",
		"[HKEY_CURRENT_USER\\Software\\Vendor\\App]\r\n",
		"@=\"default-setting\"\r\n",
		"\"Say \\\"hi\\\"\"=\"kept\"\r\n",
		"\"Notes\"=\"line one\r\nline two\"\r\n",
		"\"Blob\"=hex:01,02,\\\r\n  03\r\n",
		"\"Host\"=\"viewer.example.invalid\"\r\n",
		"[HKEY_CURRENT_USER\\Software\\Vendor\\App\\Blank]\r\n",
		"[HKEY_CURRENT_USER\\Software\\Vendor\\App\\Odd: 100% ~$]\r\n\"Mode\"=dword:00000001\r\n",
	} {
		if !strings.Contains(text, kept) {
			t.Errorf("filtered export lost %q:\n%s", kept, text)
		}
	}
	if report.KeysRemoved != 2 || report.ValuesRemoved != 5 {
		t.Errorf("report = %+v, want 2 keys and 5 values removed", report)
	}
}

// TestFilterSecretsFailsClosedOnUnclassifiableExports catches a tokenizer that
// guesses at input it does not understand. Each export below is either not
// what reg.exe writes or would change the registry beyond the captured key, so
// filtering must refuse it rather than pass any bytes through.
func TestFilterSecretsFailsClosedOnUnclassifiableExports(t *testing.T) {
	header := []string{"Windows Registry Editor Version 5.00", "", `[HKEY_CURRENT_USER\Software\Vendor\App]`}
	tests := map[string][]byte{
		"deletion section":        utf16Export(append(header, `[-HKEY_CURRENT_USER\Software\Vendor\App\Gone]`)...),
		"deletion value":          utf16Export(append(header, `"Gone"=-`)...),
		"section outside root":    utf16Export(append(header, `[HKEY_CURRENT_USER\Software\Vendor\Other]`)...),
		"unterminated string":     utf16Export(append(header, `"Notes"="no closing quote`)...),
		"unterminated hex":        utf16Export(append(header, `"Blob"=hex:01,\`)...),
		"unsupported line":        utf16Export(append(header, `Password=SECRET-unquoted`)...),
		"value before a section":  utf16Export("Windows Registry Editor Version 5.00", "", `"Password"="SECRET"`),
		"missing header":          utf16Export(`[HKEY_CURRENT_USER\Software\Vendor\App]`, `"Mode"=dword:00000001`),
		"no sections":             utf16Export("Windows Registry Editor Version 5.00", ""),
		"malformed dword":         utf16Export(append(header, `"Mode"=dword:1`)...),
		"name spanning two lines": utf16Export(append(header, `"Pass`, `word"="SECRET"`)...),
	}
	for name, export := range tests {
		t.Run(name, func(t *testing.T) {
			filtered, _, err := registryfile.FilterSecrets(export, `HKCU\Software\Vendor\App`, exampleSecretScope(t))
			if err == nil || filtered != nil {
				t.Fatalf("FilterSecrets() = %d bytes, %v; want refusal", len(filtered), err)
			}
		})
	}
}
