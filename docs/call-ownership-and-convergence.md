# Call Ownership and Convergence

Status: accepted design, implemented and verified locally.

This document defines the ownership, liveness, persistence, and convergence
rules for consumer calls controlled through ModemDeck. It is intentionally a
small single-application design. It does not provide cross-session takeover,
multi-application high availability, or distributed locking.

## Product boundary

- One user may have several authenticated sessions at the same time, such as
  local Web, public Web, another browser, and a paired iOS client.
- An authenticated session is the call-control holder boundary. Browser tabs
  that share one authenticated session are deliberately the same holder.
- The first session that successfully reserves or claims a call owns it until
  the authoritative call reaches a terminal state.
- Ownership is never transferred to another session. Losing HTTP, SSE, or
  WebRTC connectivity does not make a call available to another session.
- If the holder becomes an orphan, ModemDeck ends the call after a bounded
  recovery window. It does not release the call for takeover.
- Different holders may control different lines concurrently. One holder may
  own at most one call or pending outgoing reservation, and one line may have
  at most one owner or pending outgoing reservation.
- Application restart recovery is fail-closed. Calls already active when the
  application starts are occupied and are ended; they are never offered to a
  new holder. Preserving ownership across application restarts is out of scope.

## Authorities

The chain has one authority at each boundary:

```text
ModemManager / host Agent
    live hardware call truth
            |
            v
application call coordinator
    single order for call commands and complete snapshots
            |
            v
SQLite call projection and command journal
    committed Web/API truth, history, and request replay
            |
            +----> lifecycle: media -> recording -> ownership
            |
            v
latest-value SSE projection
            |
            v
Web / iOS live state
```

The host Agent is authoritative about whether a hardware call exists and its
phase. SQLite is authoritative for the committed projection and history. SSE
carries the current user-scoped projection but is not an independent source of
truth: reconnecting clients receive a newly built current snapshot.

## Identities

Call control uses distinct identities for distinct purposes:

| Identity | Purpose |
| --- | --- |
| authenticated session scope | call-control holder |
| authenticated user ID | account-wide credential revocation only |
| request ID | idempotent command execution and replay |
| call ID | one hardware call lifecycle |
| line ID | physical/logical line exclusion |
| media owner token | one WebRTC peer for an already-owned call |

For Web, the holder is derived from the authenticated session token already
validated by the HTTP authentication middleware. For paired mobile clients,
it is derived from the authenticated pairing credential. Raw credentials are
never exposed as holder IDs or written to logs:

```text
holderID = SHA-256("modemdeck-call-holder" || authenticated-session-scope)
```

Web sessions have no server-side time expiry. Each user retains the newest
eight sessions; a ninth successful login removes the oldest one. The Security
screen lists the browser, operating system, login host, and creation time for
each session and can revoke one session or every session except the current
one. An iOS pairing credential also has no time-based expiry, appears in the
same device list, and does not consume a Web session slot. It remains valid
until it is rotated or revoked by the account security lifecycle.

The client does not submit a separate browser or tab holder ID. A network
reconnect, page reload, or browser restart that retains the same authenticated
session therefore derives the same holder. A successful explicit Web logout
immediately marks that holder ending, closes its established media, and sends
one targeted hangup. An administrator password reset or user disable applies
the same action to every holder for that authenticated user, including paired
clients. None of these actions transfers an owned call.

Deleting a cookie only on the client, terminating the process, or dropping the
network without delivering a logout request is not a server-observed
revocation. Once both server-observed media and control paths disappear, those
indistinguishable failures use the bounded orphan grace. A deliberately
preserved live PeerConnection is still positive media liveness; detecting a
local-only cookie deletion inside that connection is an accepted edge rather
than a reason to add another client state protocol.

Request IDs remain separate from holders. A request replay is resolved from
the durable command journal before attempting a new reservation. Reusing one
request ID with a different authenticated scope or payload is rejected.

## One ownership record

The application keeps one in-memory object for an outgoing reservation and
the call that it becomes:

```go
type record struct {
    requestID string    // empty for an incoming call
    callID    string    // empty only while an outgoing call is pending
    lineID    string
    holderID  string    // empty only for a never-claimed incoming ringing call
    subjectID string    // authenticated user; used only for explicit revocation
    createdAt time.Time
    orphanAt  time.Time // cleanup deadline, never a transfer deadline
    mediaAlive bool     // observed server-side WebRTC liveness
    ending    bool
}
```

The manager owns one slice of records and one mutex. The expected number of
simultaneous lines and sessions is small, so transparent O(n) invariant checks
are preferred over several partially synchronized indexes.

The record lifecycle has these rules:

1. A pending outgoing call has a request ID and holder, but no call ID.
2. When a complete authoritative snapshot observes the resulting call, the
   same object receives its call ID. No second entry is created.
3. An incoming ringing call starts with a call ID and no holder. Claim writes
   the holder exactly once.
4. Once non-empty, holder ID is immutable until the call is terminal.
5. `orphanAt` is only a deadline for ending the call. Reaching it never clears
   holder ID and never changes the call to available.
6. `ending` is one-way. It prevents new commands other than the bounded,
   idempotent end operation.
7. Reconciliation removes the whole record only after the committed database
   projection confirms terminal or not-found following a complete Agent call
   snapshot.
8. Timer work captures the record pointer and verifies pointer identity before
   acting, so old work cannot affect a replacement object. No attempt counter
   or ownership state machine is required.
9. Revoking a pending dial only clears media liveness and keeps its ordinary
   orphan deadline. If a complete snapshot binds the call before that deadline,
   the normal expiry path ends it; there is no special reconciliation goroutine.

An outgoing reservation may be removed only after a definitive command failure
that proves no call was created. An ambiguous result retains the record until
one complete authoritative reconciliation resolves it. The resulting outgoing
call normally binds by durable request ID; if the command response was lost,
the sole pending reservation on that line may bind to the sole observed
outgoing call. If complete snapshots remain unavailable, the same `orphanAt`
deadline bounds the unresolved reservation instead of adding another state
machine.

## Projection and exclusion

Control state is derived for the authenticated requesting session; it is not
stored as a separate state machine:

1. The record holder matches the requester: `owned`.
2. The record has another holder or is ending: `occupied`.
3. Another owned, pending, orphaned, or ending record uses the same line:
   `occupied`. Only an unclaimed incoming ringing record leaves its line open
   for a first claim.
4. The requester already owns or is reserving another call: `occupied` for
   that requester.
5. Otherwise, an unclaimed incoming ringing call is `available`.
6. Every other live call is `occupied`.

Call waiting may expose several hardware calls on one line, but only one record
on that line may have a holder or pending reservation. All calls remain visible
to authorized sessions.

## Ownership and liveness

Ownership is immutable; liveness only determines whether to end the owned
call. There are two positive liveness sources.

### Media liveness

For an active media-capable call, the server-side WebRTC session is the strong
liveness signal. `connected` and the existing bounded `recovering` period are
alive. Loss of SSE or ordinary API connectivity does not affect ownership
while that media session exists.

Explicit authentication revocation is not a liveness failure. It overrides
`mediaAlive`, makes further media/control requests fail, closes the existing
PeerConnection, and starts a targeted hangup immediately without an orphan
grace.

When the media session finishes after its transport recovery window, the media
runtime notifies the ownership manager and starts the orphan deadline. The
same holder may establish a new media session before that deadline.

The media owner token remains a separate technical boundary. It prevents two
simultaneous PeerConnections for one call; it neither grants nor transfers
business call ownership.

### Control liveness

Dialing, connecting, media-unavailable, and media-recovery periods use a small
holder heartbeat endpoint independent of SSE. Any authenticated command from
the holder also refreshes control liveness. The browser sends an immediate
heartbeat and authoritative GET after reconnect, `online`, `pageshow`, or
becoming visible.

The product constant is one 15-second orphan grace. The existing
15-second WebRTC transport recovery happens before this ownership grace. The
constant is not a user setting and there is no adaptive retry state.

If neither media nor control liveness returns before `orphanAt`, the record is
marked ending and one bounded idempotent end operation is run with a stable
request ID. There is no ownership-layer retry state: if that one operation
fails, the record remains occupied/ending until authoritative hardware state
becomes terminal; it is never transferred. The normal command journal keeps
the end request idempotent if another convergence path observes the same
postcondition.

No failure detector can both tolerate an arbitrarily long partition and
identify an abandoned client immediately. The bounded grace is the explicit
product policy. Supporting indefinite recovery would require removing
automatic orphan hangup.

## Command and snapshot ordering

Every operation that can create, mutate, or close the database call projection
shares one application call-coordinator mutex:

```text
validate and resolve durable request replay
    -> acquire call coordinator
    -> ensure host-Agent control lease
    -> run Agent command when applicable
    -> obtain one complete Agent call snapshot
    -> apply call projection and command outcome in database order
    -> release coordinator
    -> publish the latest SSE state after commit
```

Background snapshots use the same coordinator. This prevents a snapshot
captured before StartCall from committing after the new call has been written.
Content hashes and wall-clock observation times are not substitutes for causal
ordering.

The request that creates an outgoing reservation is its only command
dispatcher. A concurrent retry first consults the durable journal and otherwise
reports the existing request as in progress; it never races the creator to the
Agent or releases the creator's reservation.

DTMF does not change call phase and does not publish a calls event, but it still
requires the exact holder and the Agent command gate.

For answer, reject, and hangup, Agent not-found is reconciled with one complete
snapshot. If the committed database projection is terminal/not-found, the
desired postcondition has already been reached and the idempotent command
completes successfully.

## Complete call snapshots

Absence closes a committed call only when the Agent has completely enumerated
calls for the relevant hardware state. Failure to enumerate calls must fail the
call snapshot; it must not be represented as a successful empty call list.

The database keeps the previous committed call projection when call snapshot
collection is incomplete. Once a complete snapshot commits, a missing call can
be closed immediately without an arbitrary missing-call grace.

## Agent control lease

The App-to-Agent control lease is separate from end-user ownership. It prevents
two application processes from controlling the same hardware and safely ends
calls when the controlling application is lost.

The Agent must make lease validation and a protected provider command one
operation. A command enters a shared control gate after validating the holder;
lease expiration and its snapshot/hangup cleanup enter the gate exclusively.
This prevents an expiry cleanup from taking an empty snapshot immediately
before a previously authorized StartCall creates an uncontrolled call.

An expired controller enters one cleanup-pending condition. A new controller
is installed only after one complete snapshot and targeted cleanup succeeds; a
transient snapshot failure leaves cleanup pending for the next ordinary lease
tick. This is one safety gate, not a retry state machine.

Within the ModemManager provider, complete snapshots and call/media commands
also share one mutex. This preserves their order when an App HTTP request times
out while its Agent handler is still completing the provider command.

## Persistence

SQLite persists:

- the call projection and final call history;
- request IDs, payload fingerprints, outcomes, and resource IDs in the hardware
  command journal;
- recording policy and recording artifacts;
- users, authenticated sessions, pairing credentials, and line access scope.

SQLite does not persist active call ownership or heartbeat updates. Persisting
ownership would leave a zombie holder after application restart without being
able to restore the WebRTC session.

If a future design requires several application replicas or ownership recovery
across application restart, it must add database compare-and-swap, a database
time source, and fencing tokens together. Adding only an owner column is not a
safe intermediate design.

## SSE and client reconciliation

Runtime SSE is a latest-value broadcast. Every authenticated subscriber first
receives the current user-scoped state and then newer revisions; events are not
consumed by one tab. There is no replay cursor, replay window, reset event, or
per-resource invalidation tree. A slow subscriber keeps only the newest signal.
The server builds one shared snapshot for each revision and applies each
subscriber's line and ownership scope in memory, so additional browsers do not
multiply hardware or SQLite reads.

Live calls and recording state are applied directly from SSE. The client keeps
the no-store active-call GET for initial startup, `online`, `pageshow`, becoming
visible, and ambiguous or failed call-control requests. An SSE snapshot
supersedes an older in-flight GET. A call reaching terminal state also advances
one coarse durable-data revision so paginated call history refreshes once.

Switching to WebSocket would not remove this requirement because browsers may
freeze both timers and network callbacks in background pages.

## Restart behavior

- Browser/API network reconnect with the same authenticated session resumes the
  same holder during the recovery window.
- A successful explicit Web logout, password reset, or user disable immediately
  closes media and ends each already-bound affected call. A pending dial uses
  the same 15-second orphan deadline. Neither path transfers ownership.
- A client-local cookie deletion or vanished network path that the server did
  not observe uses the 15-second orphan grace after its media and control paths
  disappear, because it is indistinguishable from a recoverable partition.
- Application restart does not recover ownership. Calls discovered during the
  initial reconciliation are occupied and are ended fail-closed.
- Before that initial complete snapshot commits, existing database calls are
  also projected as occupied and cannot be claimed.
- Agent restart and Agent controller loss end active calls through the Agent
  safety boundary.

## Required tests

The implementation keeps tests around the product invariants, rather than a
catalog of speculative races:

- one holder owns at most one call, one line has at most one holder, and other
  lines remain independent;
- same-session reconnect retains ownership and another session never takes it;
- request replay and pending-reservation binding dispatch a hardware command at
  most once;
- complete snapshots converge terminal calls, while incomplete enumeration
  preserves the prior database projection;
- media/control liveness uses the 15-second deadline and never transfers a call;
- explicit logout and administrator account revocation end the intended calls;
- a revoked pending dial binds and exits through the ordinary timeout path;
- latest-value SSE publishes after commit, coalesces slow subscribers, and an
  older GET cannot overwrite newer pushed state;
- Agent lease expiry waits for protected commands, cleans calls, then permits a
  new controller.
