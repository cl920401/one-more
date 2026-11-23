# OneMore — local P1 simulation baseline

This synthetic in-process baseline was independently checked at commit `cb08e5a5e79c672319aec201bc355b767a63d8ee` and published on the `p1-local-baseline` branch on 2026-11-23.
Read the [QA and publication receipt](evidence/QA_RECEIPT.md) for the fixed scope and evidence limits. The published repositories are [GitHub](https://github.com/cl920401/one-more/tree/p1-local-baseline) and [Gitee](https://gitee.com/miles_chen/one-more/tree/p1-local-baseline).
Whole-P1 acceptance remains incomplete. No formal first trial, real model call, customer field trial or deployment has been performed.

## Verified scope

The source baseline had two locally closed items: P1-02 (read/send separation and exact field projection) and P1-03 (scope and cancellation behavior/audit).
Eight other complete plan items remain incomplete. Explicit refusal and queued pre-send revocation have additional local evidence, without closing the whole P1-04 or P1-05 item.
The [QA receipt](evidence/QA_RECEIPT.md) records the later bounded independent review. `evidence/LOCAL_STATUS.json` is the earlier candidate-generation snapshot; its `candidate_independent_signoff=false` value is retained as history.

The fixed synthetic inputs and roles exercise separate preparation/execution authorization, exact send scopes, explicit refusal, partial drafts, distinct cancellation objects, a controlled pre-send queue, late results, byte probes, exact references, and explicit employee decisions.

The runtime has no external model adapter, network transport, telemetry, customer-file loader or service listener.
`https://unapproved.invalid` appears only as an invalid selector in a negative test.

## Reproduce locally

An installed Go toolchain with race support is required. The module targets Go 1.23; the published baseline was checked with Go 1.27.4.

```sh
sh check-local.sh
sh p1/run-local.sh demo --scenario reject
sh p1/run-local.sh demo --scenario queued-revoke
sh p1/run-local.sh demo --scenario queued-control
```

`check-local.sh` runs the 53 development tests, three copied independent-driver modules, vet, and the original/optional demos.
Dependency downloads, toolchain downloads and telemetry are disabled for these child processes.
Generated output is ignored. `ONEMORE_CHECK_CACHE` can place cache and demos outside this checkout.

The copied driver modules remain separate. Running these commands again does not create a new independent sign-off or extend the fixed-D QA scope.

## Provenance and sanitation

The verified source manifest was `d9e65ed02d25135790cf4f665534084fcba118c73f24653166707efdc0f4fa2b`.
This is a sanitized derivative: fictional personal labels became role labels, dependent assertions and six fixture fingerprints were updated, and comments no longer require private planning documents.
`SOURCE_PROVENANCE.json` records selected files and both hashes.

Independent QA subsequently checked the fixed published commit D; see the [QA receipt](evidence/QA_RECEIPT.md). `evidence/VERIFICATION.json` remains a historical candidate snapshot of the developer rerun and license review, including its earlier `publication_status="local staging only"` value. Neither historical JSON file is rewritten by this documentation follow-up.

The receipt refers to D, not to a later documentation commit; this follow-up still requires its own review.

## Limitations and license status

All identities are synthetic. Observation is within one process.
Persistent recovery/deletion, actual fee limits, full identity controls, in-transit interruption, complete editing/quality review, and method sharing/revocation are outside this baseline.
No benchmark or on-site success claim is made.

Project source is licensed under Apache-2.0; see `LICENSE` and `LICENSE_STATUS.md` for scope and provenance limitations.
