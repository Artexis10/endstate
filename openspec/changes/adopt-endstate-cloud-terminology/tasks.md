## 1. CLI help text

- [ ] 1.1 Rename the `backup` and `account` entries in `usageText` to name Endstate Cloud
- [ ] 1.2 Rename the `backup signup|login|logout|status|subscribe` and `account delete` subcommand descriptions
- [ ] 1.3 Rename the `subscribe` and `account delete` lines in the per-command usage strings
- [ ] 1.4 Confirm command tokens, flags, and help-column alignment are byte-identical

## 2. Engine strings

- [ ] 2.1 Update the `SUBSCRIPTION_REQUIRED` remediation in `internal/backup/client/errors.go`, keeping the error code and the grace/cancelled wording
- [ ] 2.2 Verify nothing parses the recovery-file header, then rename it in `internal/commands/backup_signup.go`, keeping the `#` comment prefix

## 3. Public documentation

- [ ] 3.1 Reposition the readme's "Hosted Backup" section as "Endstate Cloud", keeping the self-hosting sentence, the contract link, and the no-decryption guarantee
- [ ] 3.2 Replace "Supporter License" with "Support Endstate" in `SUPPORTERS.md`, leaving the `## Supporters` heading byte-identical
- [ ] 3.3 Name Endstate Cloud in `PRINCIPLES.md` principles 3 and 5 without weakening any commitment

## 4. Verification

- [ ] 4.1 `cd go-engine && go vet ./...`
- [ ] 4.2 `cd go-engine && go test ./...`
- [ ] 4.3 `npm run openspec:validate`
- [ ] 4.4 Grep the changed files for `Hosted Backup` and `Supporter License`, and classify every survivor as a retained identifier, required historical text, or a defect to fix
