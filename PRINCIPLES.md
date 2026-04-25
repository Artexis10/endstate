# Endstate Principles

These are the durable commitments that govern how Endstate is built and run. They constrain product decisions, pricing, and architecture. They are public so users, contributors, and future maintainers can hold the project to them.

If a future change appears to violate one of these principles, it is the principle that wins, not the change.

---

## 1. The local product is free, forever

The Endstate engine (CLI) and the Endstate GUI are free to download and use. Capture, restore, profiles, manifests, and verification work fully offline with no account required and no functional limits.

This includes the ability to back up your setup to any location you control — local disk, USB drive, Dropbox, OneDrive, Git, a self-hosted server, anywhere. The local product never restricts where you can store your own data.

Free does not mean reduced. The free product is the real product. There will never be a nag screen, a feature timeout, an artificial profile limit, or degraded local behaviour intended to push users toward payment.

## 2. The engine stays open source

The Endstate engine is licensed under Apache 2.0 and will remain so. Anyone can read it, audit it, fork it, and redistribute it.

Binary releases correspond to tagged engine commits. Anyone can build the same binary from source.

Endstate modifies your system on your behalf — installing software, restoring configurations, sometimes with elevated privileges. The only way to know what software with that capability is doing is to read the code. Open source isn't a marketing posture for a product like this; it's the minimum bar for trust.

## 3. Subscription only gates access to Endstate's managed services

Endstate offers paid hosted services — currently encrypted backup hosting, possibly continuous sync in the future. These cost money because running them costs money: storage, bandwidth, authentication, uptime, security.

A subscription buys access to those managed services. It does not gate anything else.

Every part of Endstate that runs on your own machine — capture, restore, profiles, manifests, verification, and backup to locations you control — works without a subscription, forever.

If you want the convenience of Endstate operating the storage and encryption infrastructure for you, you pay for that service. If you want to operate that infrastructure yourself or use storage you already have, the product fully supports that and always will.

## 4. Hosted data is encrypted end-to-end with client-side keys

Data uploaded to Endstate's managed services is encrypted on your machine before it leaves. The server stores ciphertext and metadata only.

Endstate cannot read your data. Endstate cannot decrypt it under subpoena, subpoena threat, breach, or internal request. The keys live with you.

This is non-negotiable and architectural, not a policy that can be quietly changed.

## 5. Self-hosting is a supported pattern

The hosted backup protocol is documented and open. Anyone can run their own backup server. The Endstate client supports configuring an alternative server endpoint.

Endstate's paid hosting competes with self-hosting on convenience, not lock-in. Users who prefer to host their own backups should be able to, indefinitely.

## 6. No account required for local use

Creating an account is optional, tied only to paid hosted services, and never required to install, run, or use the local product.

The local product does not connect to Endstate servers in the background. Network connections happen only when you take an action that requires them. Local features never check subscription status against any server. License activation for paid tiers occurs once online and the resulting token works offline forever.

## 7. No telemetry without explicit consent

Endstate does not collect usage analytics, crash reports, or behavioural data by default. If telemetry is ever added it will be opt-in, anonymous, clearly described, and disable-able with a single toggle.

---

## What this means in practice

These principles constrain real decisions. Some examples of things they rule out:

- A "free trial" model where the local product expires
- A free tier that limits the number of profiles or apps
- Local features locked behind subscription status
- Telemetry enabled by default with an opt-out
- An account requirement for using local capture or restore
- Reading user data server-side for any purpose, including support
- Removing self-hosting capability to drive subscription conversion
- Migrating existing free functionality behind a paywall

These trade-offs are intentional. They limit short-term revenue extraction in exchange for long-term trust. The project is built with the assumption that trust compounds and extraction does not.

---

## Changes to these principles

These principles can change, but only by addition or strengthening — never by weakening.

A new principle that adds a commitment is fine. A change that removes or softens an existing commitment is a breach of the project's contract with its users.

If circumstances ever require a genuine breach (for example, legal requirements that conflict with a principle), the project will say so publicly, explain the conflict, and let users decide whether to continue trusting it. There will not be quiet erosion.

---

*Last updated: 2026-04-25*
