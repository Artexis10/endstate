## 1. Contract

- [x] 1.1 Document the same-origin redirect policy and explicit endpoint exception in the hosted-backup contract.

## 2. Implementation

- [x] 2.1 Configure the default backup API client to reject cross-origin redirects without leaking query values.
- [x] 2.2 Configure the default OIDC client to reject cross-origin redirects without leaking query values.
- [x] 2.3 Preserve same-origin redirects.

## 3. Tests

- [x] 3.1 Test that login, signup, recovery, and create-version POST bodies do not reach a cross-origin 307/308 target.
- [x] 3.2 Test that OIDC discovery does not reach a cross-origin redirect target and its error is redacted.
- [x] 3.3 Test same-origin redirects for both default clients.
