## 1. Discovery + auth types

- [ ] 1.1 Add optional `AuthClaimEndpoint string \`json:"auth_claim_endpoint,omitempty"\`` field to `oidc.EndstateExtensions` in `internal/backup/oidc/oidc.go`. Do NOT add to `validateDocument` required-fields check.
- [ ] 1.2 Add optional `Email string \`json:"email,omitempty"\`` field to `auth.CompleteLoginResponse` in `internal/backup/auth/authenticator.go`.
- [ ] 1.3 Add `auth.ClaimBody` struct (= `SignupBody` minus `Email`) immediately after `SignupBody` in `internal/backup/auth/authenticator.go`.
- [ ] 1.4 Add `Authenticator.Claim(ctx, claimToken, body) (*CompleteLoginResponse, *envelope.Error)` mirroring `Signup` with `Headers: http.Header{"Authorization": []string{"Bearer " + claimToken}}` + `SkipAuthRefresh: true` + issuer-relative URL fallback.

## 2. Error code passthrough

- [ ] 2.1 Extend the post-decode switch in `parseAPIError` (`internal/backup/client/client.go`) to passthrough `CLAIM_TOKEN_INVALID`, `CLAIM_TOKEN_EXPIRED`, `CLAIM_TOKEN_CONSUMED`, and `KDF_TOO_WEAK` as `envelope.ErrorCode(strings.ToUpper(env.Error.Code))`.
- [ ] 2.2 Add a short comment in `internal/envelope/errors.go` documenting that the four substrate claim codes are passed through as valid `ErrorCode` values without being declared as Go constants (engine is a transparent pipe for substrate's domain codes).

## 3. CLI flag plumbing

- [ ] 3.1 Add `Token string` field to `commands.BackupFlags` in `internal/commands/backup.go`.
- [ ] 3.2 Add `--token` parser case in `cmd/endstate/main.go` (mirror the `--email` block at ~line 231-235).
- [ ] 3.3 Add `Token: p.token` field to the `commands.RunBackup` flag passthrough in `cmd/endstate/main.go` (~line 498-511).
- [ ] 3.4 Add `token string` field to `parsedArgs` struct in `cmd/endstate/main.go`.
- [ ] 3.5 Append `claim --token <token> --save-recovery-to <path>` line to the `backup` subcommand usage blurb in `cmd/endstate/main.go` (`commandUsage("backup")`).

## 4. Claim handler

- [ ] 4.1 Create `internal/commands/backup_claim.go` with `runBackupClaim(flags BackupFlags) (interface{}, *envelope.Error)`.
- [ ] 4.2 Validate `--token`: non-empty, length 43, charset `[A-Za-z0-9_-]`. Fail with `ErrInternalError` + remediation before any network call.
- [ ] 4.3 Reuse `signupReader(os.Stdin)`, mnemonic gen/parse, salt + DEK gen, `crypto.DeriveKeys`, `crypto.WrapDEK`, recovery key + verifier + wrapping, `writeRecoveryFile` — identical to `runBackupSignup` lines 56-150.
- [ ] 4.4 Build `auth.ClaimBody` (no email field), call `a.Claim(ctx, flags.Token, body)`.
- [ ] 4.5 On success, populate `SignupResult.Email` from `strings.ToLower(resp.Email)` (server-supplied), persist DEK + wrappedDEK + zero local DEK identical to signup.
- [ ] 4.6 Add `case "claim": return runBackupClaim(flags)` to the dispatch in `internal/commands/backup.go`. Add `claim` to the empty-subcommand error message.

## 5. Tests

- [ ] 5.1 Create `internal/commands/backup_claim_test.go` with a `newClaimBackend(t)` fixture mounting `/api/auth/claim` on a fresh mux + `httptest.Server` (reuse `addAuthRoutes`).
- [ ] 5.2 `TestBackupClaim_HappyPath`: 200 + `{userId, email, accessToken, refreshToken, subscriptionStatus}`. Assert: result has server-supplied email, recovery file written, refresh + DEK in keychain, request had `Authorization: Bearer <token>` header, request body has NO `email` field.
- [ ] 5.3 `TestBackupClaim_RequiresToken`: omit `--token` → `ErrInternalError` mentioning `--token`.
- [ ] 5.4 `TestBackupClaim_RejectsMalformedToken`: `--token=too-short` → `ErrInternalError` mentioning "43 characters".
- [ ] 5.5 `TestBackupClaim_RequiresSaveRecoveryToWhenGenerating`: empty mnemonic + empty `--save-recovery-to` → `ErrInternalError`.
- [ ] 5.6 `TestBackupClaim_RequiresPassphrase`: empty passphrase → `ErrInternalError`.
- [ ] 5.7 `TestBackupClaim_ClaimTokenInvalid_401`: 401 + body code `CLAIM_TOKEN_INVALID` → envelope `Code == "CLAIM_TOKEN_INVALID"`.
- [ ] 5.8 `TestBackupClaim_ClaimTokenExpired_401`: 401 + body code `CLAIM_TOKEN_EXPIRED` → `Code == "CLAIM_TOKEN_EXPIRED"`.
- [ ] 5.9 `TestBackupClaim_ClaimTokenConsumed_409`: 409 + body code `CLAIM_TOKEN_CONSUMED` → `Code == "CLAIM_TOKEN_CONSUMED"`.
- [ ] 5.10 `TestBackupClaim_KdfTooWeak_400`: 400 + body code `KDF_TOO_WEAK` → `Code == "KDF_TOO_WEAK"`.
- [ ] 5.11 `TestBackupClaim_RecoveryFileWriteFailureNoNetworkCall`: unwritable `--save-recovery-to` → `ErrInternalError`, claim hits == 0.
- [ ] 5.12 Mark heavy-KDF scenarios as `testing.Short()`-aware, mirroring `backup_signup_test.go`.

## 6. Verify

- [ ] 6.1 `go test ./internal/commands/... ./internal/backup/...` — full pass on the engine repo.
- [ ] 6.2 `go test ./internal/commands/ -run TestBackupClaim` — every new scenario green.
- [ ] 6.3 `go build ./...` — clean compile.
- [ ] 6.4 `openspec validate add-backup-claim-subcommand --strict` — passes.
- [ ] 6.5 Manual smoke (post-merge, both engine + GUI ready): build engine, drop into GUI repo via predev rebuild, run `npm run tauri dev`, paste a real claim code, confirm credential attachment + backup pane lands signed in.
