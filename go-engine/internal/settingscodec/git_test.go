// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package settingscodec

import (
	"strings"
	"testing"
)

func TestGitConfigSafeCodecDropsCredentialAndExecutableSurfaces(t *testing.T) {
	input := []byte(`# copied comments can contain ghp_comment_secret and are not portable
[user]
  name = Example User
  email = person@example.test
[credential "https://example.test"]
  helper = store
  password = literal-secret
[http "https://example.test"]
  sslVerify = true
  extraHeader = Authorization: Bearer header-secret
[includeIf "gitdir:~/work/"]
  path = ~/.gitconfig-work
[alias]
  co = checkout
  publish = !curl -H "Authorization: Bearer alias-secret" https://example.test
[url "https://url-secret@example.test/"]
  insteadOf = upstream:
[core]
  editor = nvim # harmless comment is discarded
  sshCommand = ssh -i ~/.ssh/id_work
[filter "danger"]
  process = /tmp/run-filter
[diff "danger"]
  command = /tmp/run-diff
[merge "danger"]
  driver = /tmp/run-merge %O %A %B
[sendemail]
  smtpPass = unmarked-value
[submodule "danger"]
  update = !/tmp/run-submodule
`)

	got, err := Transform(GitConfigSafeV1, "${home}/.gitconfig", input)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		"[user]", "name = Example User", "email = person@example.test",
		`[http "https://example.test"]`, "sslVerify = true",
		"[alias]", "co = checkout", "[core]", "editor = nvim",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("sanitized config omitted %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{
		"ghp_comment_secret", "credential", "literal-secret", "extraHeader",
		"header-secret", "includeIf", "gitconfig-work", "publish", "alias-secret",
		"url-secret", "sshCommand", "id_work", "harmless comment",
		"run-filter", "run-diff", "run-merge", "smtpPass", "unmarked-value", "run-submodule",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("sanitized config retained %q:\n%s", forbidden, text)
		}
	}
}

func TestGitConfigSafeCodecFailsClosedOnMalformedOrSensitiveContinuation(t *testing.T) {
	for name, input := range map[string]string{
		"malformed section":         "[user\nname = Example\n",
		"key outside section":       "password = exposed\n",
		"unterminated continuation": "[user]\nname = Example \\",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Transform(GitConfigSafeV1, "${xdg.config}/git/config", []byte(input)); err == nil {
				t.Fatal("unsafe Git config was accepted")
			}
		})
	}
}

func TestGitConfigSafeCodecPassesNonConfigCompanionFilesExactly(t *testing.T) {
	input := []byte("*.swp\n.env\n")
	got, err := Transform(GitConfigSafeV1, "${xdg.config}/git/ignore", input)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(input) {
		t.Fatalf("companion file = %q", got)
	}
}

func TestTransformRejectsUnknownCodec(t *testing.T) {
	if _, err := Transform("wishful-v1", "${home}/file", []byte("state")); err == nil {
		t.Fatal("unknown codec was accepted")
	}
}

func TestGitConfigSafeCodecRejectsSameNamedUnreviewedTarget(t *testing.T) {
	if _, err := Transform(GitConfigSafeV1, "${xdg.config}/other/config", []byte("[user]\nname = Example\n")); err == nil {
		t.Fatal("Git codec accepted a target outside its reviewed live layouts")
	}
}
