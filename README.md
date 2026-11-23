# OneMore — local P1 baseline candidate

This synthetic in-process prototype is prepared for the local branch `p1-local-baseline`.
It is a staging candidate. No push, merge, tag, release, real model call, field trial, or deployment is included.

## Verified scope

The source baseline had two locally closed items: P1-02 (read/send separation and exact field projection) and P1-03 (scope and cancellation behavior/audit).
Eight other complete plan items remain incomplete. Explicit refusal and queued pre-send revocation have additional local evidence, without closing the whole P1-04 or P1-05 item.
See `evidence/LOCAL_STATUS.json` for bounded status.

The fixed synthetic inputs and roles exercise separate preparation/execution authorization, exact send scopes, explicit refusal, partial drafts, distinct cancellation objects, a controlled pre-send queue, late results, byte probes, exact references, and explicit employee decisions.

The runtime has no external model adapter, network transport, telemetry, customer-file loader or service listener.
`https://unapproved.invalid` appears only as an invalid selector in a negative test.

## Reproduce locally

An installed Go toolchain with race support is required. The module targets Go 1.23; this candidate was checked with Go 1.27.4.

```sh
sh check-local.sh
sh p1/run-local.sh demo --scenario reject
sh p1/run-local.sh demo --scenario queued-revoke
sh p1/run-local.sh demo --scenario queued-control
```

`check-local.sh` runs the 53 development tests, three copied independent-driver modules, vet, and the original/optional demos.
Dependency downloads, toolchain downloads and telemetry are disabled for these child processes.
Generated output is ignored. `ONEMORE_CHECK_CACHE` can place cache and demos outside this checkout.

The copied driver modules remain separate. A staging rerun does not constitute a new independent sign-off.

## Provenance and sanitation

The verified source manifest was `d9e65ed02d25135790cf4f665534084fcba118c73f24653166707efdc0f4fa2b`.
This is a sanitized derivative: fictional personal labels became role labels, dependent assertions and six fixture fingerprints were updated, and comments no longer require private planning documents.
`SOURCE_PROVENANCE.json` records selected files and both hashes.

Original QA conclusions apply to the original build. This candidate has its own manifest and staging checks and needs review of the sanitation changes before publication.

## Limitations and license status

All identities are synthetic. Observation is within one process.
Persistent recovery/deletion, actual fee limits, full identity controls, in-transit interruption, complete editing/quality review, and method sharing/revocation are outside this candidate.
No benchmark or on-site success claim is made.

Project source is licensed under Apache-2.0; see `LICENSE` and `LICENSE_STATUS.md` for scope and provenance limitations.
