# Local QA and publication receipt

Date: 2026-11-23 (UTC).

## Fixed object and independent scope

The independent local QA result applies only to commit D:
cb08e5a5e79c672319aec201bc355b767a63d8ee.
Its sole parent is the README-only main commit S:
b2892850137f2ee4e1857bb1439a180f9a348a09.

The bounded synthetic baseline passed race-enabled tests and vet in four modules:
p1, qa/p1-audit-repair, qa/p1-remaining and qa/p1-local-gates.
Five demonstration groups were checked: original, adoption, reject,
queued-revoke and queued-control. Their local end/drain signals were checked.

This is a local technical result, not acceptance of the whole P1 plan.
Only P1-02 and P1-03 have the stated local closure; the other eight complete
items remain incomplete. Additional refusal and queued-revocation evidence
does not close the whole P1-04 or P1-05 item.

## Publication observation

On 2026-11-23, actual pushes and subsequent Git readback succeeded for both
[GitHub](https://github.com/cl920401/one-more) and
[Gitee](https://gitee.com/miles_chen/one-more).
Each advertised default HEAD was refs/heads/main at S, and
refs/heads/p1-local-baseline was at D. No additional refs or tags were
advertised in those readbacks. This records that observation, not a live check.

## Evidence limits and history

The checks used fixed synthetic inputs and in-process simulation.
They do not prove production identity isolation, external transport behavior,
durable recovery or deletion, actual fee enforcement, complete privacy
coverage, benchmark quality, authorship or chain of title.
No formal first trial, real model call, customer field trial or deployment
was performed. No merge, pull request, tag or release was created.

LOCAL_STATUS.json and VERIFICATION.json are preserved candidate snapshots.
Their earlier independent-signoff, developer-rerun and publication fields
record that earlier stage; this receipt supplies the later bounded result.
This receipt does not independently sign off a later documentation commit.
