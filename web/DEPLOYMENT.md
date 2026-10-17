# Stable web origin and signed relay discovery

Status (2026-10-17): the static app is published at https://iroshcha.github.io/blizko/.
The directory branch is published, but automatic server publication is NOT activated:
a permanent GitHub write deploy key is awaiting explicit owner approval.
A clean browser cannot yet connect from Pages to free Tuna: its abuse interstitial
intercepts both WebSocket and HTTP CORS preflight. Do not instruct users to migrate
their profile to Pages until a compatible free tunnel has passed end-to-end tests.
The existing Tuna /web/ client remains usable after its normal visitor confirmation.
Windows/Android 0.3.2 use native WebSocket and are not affected by this browser gate.

## Data boundaries

GitHub Pages holds only static UI files. Browser profiles, encryption keys, contacts
and history remain in IndexedDB under that origin. An existing Tuna browser profile
requires one password-protected export/import to move to Pages. A changed relay URL
after that does not change the browser origin or identity. Native app data is unchanged.

Clients fetch https://raw.githubusercontent.com/iroshcha/blizko/relay-address/relay.json
at most once per minute. The small envelope contains a base64 JSON payload and an
Ed25519 signature over `blizko-relay-directory-v1` + NUL + payload. The public key is
pinned in core/discovery and web/src/discovery.js. Records contain only version,
HTTPS root URL, issuance time (revision) and expiry; validity is at most eight days.
Invalid signatures, expired records, unsafe URLs and observed revision rollback are
rejected. Native clients retain an unexpired cached record or the built-in current
Tuna URL during a directory outage. Custom manually configured servers are preserved.
No profile data is sent to GitHub. GitHub Pages and repository write access remain
part of the trust boundary for web application code, independently of record signing.

## Build and publish

1. In web: `npm ci --ignore-scripts`, `npm test`, `node build.mjs`, `node build-pages.mjs`.
2. Publish only dist/pages to gh-pages; Pages source is gh-pages root.
3. Build core/server and allow the exact comma-separated origins of the embedded app
   and `https://iroshcha.github.io`. Never infer allowed origins from request headers.
4. Build tools/relay-publisher as its separate Go module. go-git is not linked into
   Android or the desktop engine.
5. On the server, initialize publisher-private with -init, under ACLs restricted to
   the server account, SYSTEM and Administrators. Private keys stay on the server.
6. Register its public SSH key as a write deploy key ONLY after owner approval.
   GitHub deploy keys grant repository-wide write access, not branch-only access.
7. Bootstrap relay-address with an exported signed relay.json. The publisher requires
   this branch; it never force-pushes or creates/replaces another branch.
8. Install Blizko.RelayPublisher.exe at the server root to activate the existing
   supervisor loop (settings.pagesOrigin must be configured). The process reads only
   the current Tuna log, writes its private checkout and publishes relay.json.
   Without the root executable it remains disabled.

Publisher SSH uses ssh.github.com:443 and pins GitHub's Ed25519 host fingerprint.
It signs seven-day records, refreshes after two days, checks the tunnel each minute,
and avoids GitHub requests while the last successful record is still fresh and the
address unchanged. A failure is retried on the next supervisor pass. Logs contain
public URLs and errors, never keys. Preserve publisher-private alongside the existing
private server data; replacing its signing key requires new client builds.

## Verification and limits

Node tests cover signatures, URL restrictions, expiry, cached outage and rollback.
Go tests cover verification and reconnecting between two brokers without changing
identity/keys. Production native/web interoperability is checked through Tuna.
The Pages-to-Tuna clean-browser check currently fails at Tuna's warning page; a
working origin allowlist alone cannot resolve that provider restriction.
There is no uptime guarantee from the free providers or the home network.
The native visual redesign is not included in version 0.3.2.

## References used

Build Your Own X is a tutorial catalog, not a mandate to replace mature libraries:
https://github.com/codecrafters-io/build-your-own-x
The linked browser-security tutorial informed the separate origin/transport design:
https://aosabook.org/en/500L/the-same-origin-policy.html
Official Pages documentation: https://docs.github.com/en/pages/getting-started-with-github-pages/what-is-github-pages
Tuna warning behavior: https://tuna.am/en/docs/tunnels/http/
GitHub SSH fingerprints: https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints