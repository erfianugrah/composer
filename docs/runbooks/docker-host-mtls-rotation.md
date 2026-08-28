# Rotating mTLS certificates for a remote docker host

Composer connects to remote docker daemons over mTLS. Client certs are stored
per host - either as files under `cert_dir` (legacy) or in the database
(uploaded via the UI / API, encrypted at rest, materialized to
`<dataDir>/certs/<host_id>/` on demand). DB certs win when both exist.

This runbook covers rotating a host's client certificate without breaking
deploys. The failure mode you are avoiding: the server (dockerd, or an
mTLS gateway in front of it) starts rejecting the old client cert before
composer has the new one - every stack operation on that host then fails.

## Safe rotation order

1. **Issue the new client cert** from the CA the server trusts. If the CA
   itself is rotating, the server must trust BOTH CAs during the transition
   (most gateways and dockerd accept a CA bundle file - append the new CA,
   reload the server).
2. **Upload to composer first**: Settings -> Docker Hosts -> edit the host ->
   mTLS certificates section. Paste new CA cert, client cert, client key (all
   three - the upload is validated as a set: PEM parse, cert-key match, chain
   verify). The save invalidates the cached docker client, so the next
   operation uses the new material immediately. Confirm with the row's
   **Test** button - expect `ok <n>ms`.
3. **Deploy a no-op** against a stack on that host (or just watch the stacks
   list, which hits the host on the next status refresh) to confirm traffic
   flows with the new cert.
4. **Only now revoke the old cert / remove the old CA** from the server side.

Reversing steps 2 and 4 is the outage: server rejects old, composer still
presents old.

## Checking expiry

The Docker Hosts table shows `cert exp <date>` under the mTLS badge when the
host has stored certs. `GET /api/v1/hosts/{id}/certs` returns the fingerprint
and `not_after` without exposing key material. Rotate ahead of expiry - a
lapsed client cert fails every operation on the host exactly like a revoked
one.

## Recovering from a broken upload

The upload endpoint rejects PEM that fails validation (422), so a bad paste
cannot wedge the host - the previous certs stay active. If you somehow end up
with certs stored that the server rejects (e.g. rotated the server CA first),
fix by either:

- uploading the correct triple via the UI (works even while the host is
  unreachable - the save does not dial the daemon), or
- `DELETE /api/v1/hosts/{id}/certs` to fall back to the legacy `cert_dir`
  material on the composerd host filesystem, if it is still present.

The **Test** button is the source of truth after any change: it builds a
throwaway client with the current material (DB certs > cert_dir > plain) and
pings with a 3s timeout, without touching the cached client used by deploys.
