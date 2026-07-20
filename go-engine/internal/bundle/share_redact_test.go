// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactText_ReplacesUserPathSegment(t *testing.T) {
	got, counts := redactText(`{"recent":"C:\\Users\\hugo\\Documents\\x.txt"}`, "", false)

	if strings.Contains(got, "hugo") {
		t.Errorf("username survived: %s", got)
	}
	if !strings.Contains(got, `C:\\Users\\REDACTED\\Documents`) {
		t.Errorf("path shape not preserved: %s", got)
	}
	if counts[RedactRuleUserPath] != 1 {
		t.Errorf("user-path count = %d, want 1", counts[RedactRuleUserPath])
	}
}

func TestRedactText_ReplacesEmailAndHostname(t *testing.T) {
	got, counts := redactText("contact hugo@example.com on DESKTOP-ABC123", "DESKTOP-ABC123", false)

	if strings.Contains(got, "hugo@example.com") {
		t.Error("email survived")
	}
	if strings.Contains(got, "DESKTOP-ABC123") {
		t.Error("hostname survived")
	}
	if counts[RedactRuleEmail] != 1 || counts[RedactRuleHostname] != 1 {
		t.Errorf("counts = %v", counts)
	}
}

// TestRedactText_GitUserFieldsStripped covers the flagship case: a shared
// .gitconfig would otherwise carry the sender's identity into every commit the
// recipient makes.
func TestRedactText_GitUserFieldsStripped(t *testing.T) {
	in := "[user]\n\tname = Hugo Ander\n\temail = hugo@example.com\n\tsigningkey = ABC123\n[core]\n\tautocrlf = true\n"

	got, counts := redactText(in, "", true)

	for _, leak := range []string{"Hugo Ander", "hugo@example.com", "ABC123"} {
		if strings.Contains(got, leak) {
			t.Errorf("%q survived: %s", leak, got)
		}
	}
	// Preferences are the point of sharing and must survive.
	if !strings.Contains(got, "autocrlf = true") {
		t.Errorf("non-identity setting was destroyed: %s", got)
	}
	if counts[RedactRuleGitUser] != 3 {
		t.Errorf("git-user count = %d, want 3", counts[RedactRuleGitUser])
	}
}

// TestRedactText_IsIdempotent guards against a second pass mangling its own
// output or inflating counts.
func TestRedactText_IsIdempotent(t *testing.T) {
	once, _ := redactText(`C:\Users\hugo\x hugo@example.com`, "HOST", false)
	twice, counts := redactText(once, "HOST", false)

	if once != twice {
		t.Errorf("second pass changed the text:\n  1: %s\n  2: %s", once, twice)
	}
	for rule, n := range counts {
		if n != 0 {
			t.Errorf("second pass counted %d for %s; already-redacted text should be inert", n, rule)
		}
	}
}

// TestRedactText_DocumentedMisses pins the leak boundary. These are known,
// deliberate gaps — a redaction feature that quietly misses things invites trust
// it has not earned, so they are asserted rather than left to discovery.
func TestRedactText_DocumentedMisses(t *testing.T) {
	t.Run("bare username outside a path context is not replaced", func(t *testing.T) {
		got, _ := redactText(`{"lastUser":"hugo"}`, "", false)
		if !strings.Contains(got, "hugo") {
			t.Skip("behaviour tightened; update the documented boundary")
		}
	})

	t.Run("licence-key shapes are not replaced", func(t *testing.T) {
		key := "XXXXX-YYYYY-ZZZZZ-11111"
		got, _ := redactText("key="+key, "", false)
		if !strings.Contains(got, key) {
			t.Skip("behaviour tightened; update the documented boundary")
		}
	})

	t.Run("paths outside a Users directory are not replaced", func(t *testing.T) {
		p := `D:\clients\acme\notes.txt`
		got, _ := redactText(p, "", false)
		if !strings.Contains(got, "acme") {
			t.Skip("behaviour tightened; update the documented boundary")
		}
	})
}

// ---------------------------------------------------------------------------
// Tree walk
// ---------------------------------------------------------------------------

func TestRedactShareTree_ReportsUnscannedBinaries(t *testing.T) {
	root := t.TempDir()
	stageConfig(t, root, "app/settings.json", `{"path":"C:\\Users\\hugo\\x"}`)

	// A NUL byte marks this as binary in practice; it must be left alone AND
	// reported, so identity inside it is a known unknown rather than a silent one.
	binPath := filepath.Join(root, "configs", "app", "state.db")
	if err := os.WriteFile(binPath, []byte{0x00, 0x01, 'h', 'u', 'g', 'o'}, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := redactShareTree(root, "")
	if err != nil {
		t.Fatalf("redactShareTree: %v", err)
	}

	if len(report.Unscanned) != 1 || !strings.HasSuffix(report.Unscanned[0], "state.db") {
		t.Errorf("unscanned = %v, want the binary listed", report.Unscanned)
	}
	after, _ := os.ReadFile(binPath)
	if len(after) != 6 || after[0] != 0x00 {
		t.Error("binary payload was modified; unscanned files must be left byte-identical")
	}
	if report.FilesChanged != 1 || report.Rules[RedactRuleUserPath] != 1 {
		t.Errorf("expected the text file redacted; report = %+v", report)
	}
}

// TestRedactShareTree_PreservesUTF16BOM covers Windows registry exports, which
// are UTF-16LE with a BOM and carry user paths. Rewriting one as UTF-8 would
// leave a file the consuming application cannot read.
func TestRedactShareTree_PreservesUTF16BOM(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "configs", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	regPath := filepath.Join(dir, "export.reg")
	if err := os.WriteFile(regPath, encodeText(`"Path"="C:\\Users\\hugo\\App"`, encodingUTF16LE), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := redactShareTree(root, ""); err != nil {
		t.Fatalf("redactShareTree: %v", err)
	}

	raw, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xFE {
		t.Fatal("UTF-16LE BOM was not preserved")
	}
	text, _, ok := decodeText(raw)
	if !ok {
		t.Fatal("rewritten file no longer decodes")
	}
	if strings.Contains(text, "hugo") {
		t.Errorf("username survived in UTF-16 payload: %s", text)
	}
}

func TestShareModuleDenied(t *testing.T) {
	if !ShareModuleDenied("apps.thunderbird") {
		t.Error("a mail client's profile is account-bound and must not be shared")
	}
	if !ShareModuleDenied("APPS.ANYDESK") {
		t.Error("deny matching should be case-insensitive")
	}
	if ShareModuleDenied("apps.vscode") {
		t.Error("an ordinary preferences module must not be denied")
	}
}

// TestRedactText_HandlesFileURIs covers percent-encoded drive colons.
//
// Found by scanning a real share bundle: a VSCodium extensions.json leaked the
// username as "file:///c%3A/Users/<name>/..." after every plain path in the same
// bundle had been redacted. Editors store file URIs routinely, so this is a
// common shape, not an exotic one.
func TestRedactText_HandlesFileURIs(t *testing.T) {
	in := `{"external":"file:///c%3A/Users/hugo/.vscode-oss/extensions/pkg"}`

	got, counts := redactText(in, "", false)

	if strings.Contains(got, "hugo") {
		t.Errorf("username survived in a file URI: %s", got)
	}
	if !strings.Contains(got, "c%3A/Users/REDACTED/") {
		t.Errorf("URI shape not preserved: %s", got)
	}
	if counts[RedactRuleUserPath] != 1 {
		t.Errorf("user-path count = %d, want 1", counts[RedactRuleUserPath])
	}
}
