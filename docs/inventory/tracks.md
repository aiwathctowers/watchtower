# Behavior Inventory — Tracks

> Each item below is a **behavioral contract** that must be preserved.
> Modifying or weakening the protecting test requires explicit approval
> from @Vadym.
>
> AI assistant: when working in `internal/tracks/`, `internal/db/tracks.go`,
> or `WatchtowerDesktop/Sources/Views/Tracks/`, read this file first. Any
> proposed change that would break a guard test or remove a contract must
> be raised as a question before touching code.

**Module:** `internal/tracks/` + `internal/db/tracks.go` + `WatchtowerDesktop/Sources/Views/Tracks/`
**Last full audit:** 2026-04-28

## What a track is and what it's built from

**What it is.** A track is a single narrative row representing one ongoing situation the user should keep an eye on — a decision being formed, a thread waiting on a reply, a slow-burn incident, a piece of work spanning multiple channels. It is **not** a task: there is no `done` / `resolved` / `archived` state, no checkbox, no due date. A track stays "active" until the user explicitly dismisses it (TRACKS-07); the pipeline's job is to keep the track's `text`, `current_status`, `participants`, `priority`, and `category` current as the situation evolves — not to push it through a workflow. One row per situation, regardless of how many channels, threads, or days it spans.

**What it's built from.** Tracks are not derived from raw messages. They sit downstream of the channel-digest pipeline and consume already-summarised signals:

- `digests.situations` (per-channel MAP output) — participants with roles (driver / reviewer / blocker), dynamics, outcomes, red flags, action items.
- `digests.topics` — topic-level summaries with `key_messages` and timestamps; these are the primary unit the LLM is asked to convert into tracks.
- `digests.running_summary` — the channel's compact rolling memory (active topics, recent decisions, open questions). Injected as `=== CHANNEL CONTEXT ===` so the model can recognise threads the channel has already exposed and avoid re-proposing them as fresh tracks.
- `tracks` + each track's narrative fields — passed back to the AI as `existing_tracks` via `formatExistingTracks`. This is what makes the model's first move "should this fold into something I already track?" rather than "should I create a new one?". This is the merge-over-split bias.
- `user_profile`: starred channels, declared reports/peers, role. Drives `scoreChannel` (TRACKS-02) and the watching-lane filter (TRACKS-03) — i.e. _which_ channels and _which_ flavours of topic are even allowed to surface.
- Cross-cycle dedup marker: digest topics already linked to a track via `source_refs` (`digest_id`+`topic_id`) are stripped from the prompt before extraction, so the model cannot re-propose them (TRACKS-01).

The pipeline issues **one AI call per `Run()`** that receives all of the above and returns `{new_tracks: [...], updated_tracks: [...]}`. New tracks then run through fingerprint + Jaccard text dedup before they are persisted; updates are gated by the cross-user owner check (TRACKS-05).

**Why this is the design (the point).** The default Slack surface is N channels of noise the user has to scroll through. Tracks invert that surface by compressing everything to "the K things that need your eye right now". That compression is only worth anything if the user trusts the count — and trust is exactly what every contract below is protecting:

- the count must _mean_ something — re-extraction must not inflate it (TRACKS-01, TRACKS-06 channel/digest part);
- silence must be possible — a chatty channel the user doesn't engage with must produce zero tracks (TRACKS-02);
- the manager use case must not collapse back into noise — "watching" stays narrow (TRACKS-03);
- read state must be honest — once a thing is seen it stays seen until something genuinely new lands (TRACKS-04);
- an AI hallucination must not be able to rewrite the wrong user's track (TRACKS-05);
- history must not be silently destroyed by a re-extraction cycle (TRACKS-06 state-history part);
- dismiss must actually mean dismiss (TRACKS-07).

If any one of these breaks, the user stops trusting the count — and a track surface they don't trust is functionally identical to no track surface at all. That is why the contracts below are load-bearing rather than nice-to-have.

## TRACKS-01 — One situation, one track

**Status:** Enforced

**Observable:** The same conversation, decision, or piece of work never appears twice in the tracks list. Re-extracting yesterday's digests doesn't grow the feed by N copies of yesterday. When the same thread surfaces again across cycles or channels, it merges into the existing track instead of creating a new one. Specifically:

- AI may identify an `existing_id` to update — that update sticks (subject to ownership, see TRACKS-05).
- A `[CEX-1234] / CVE-2026-… / MR!4567 / U-id / IP-addr` mentioned in both old and new content fingerprint-matches and merges.
- Russian/English text+context with Jaccard similarity ≥ 0.30 (using a 5-rune pseudo-stem so `инцидент` / `инцидента` / `инциденту` collapse to the same token) merges.
- Digest topics already linked to a track via `source_refs` (`digest_id`+`topic_id`) are stripped from the AI prompt before extraction, so the model can't propose a duplicate from them.

**Why locked:** The whole value of Tracks is "the single line per thing I owe". Without the four-layer dedup the feed doubles every daemon cycle (~every few minutes), the user loses trust in the count, and the read/unread surface becomes meaningless because every read item resurfaces under a new ID. This is the hardest contract to keep alive against AI prompt drift — small wording changes silently break it.

**Test guards:**
- `internal/tracks/pipeline_test.go::TestFindSimilarTrack`
- `internal/tracks/pipeline_test.go::TestTextSimilarityDedupInStoreTrackItems`
- `internal/tracks/pipeline_test.go::TestJaccardSimilarity`
- `internal/tracks/pipeline_test.go::TestTokenizeText`
- `internal/tracks/pipeline_test.go::TestTopicDedupBySourceRefs`
- `internal/db/tracks_test.go::TestFindTracksByFingerprint`

**Locked since:** 2026-04-28

## TRACKS-02 — Silent channels stay silent

**Status:** Enforced

**Observable:** A channel I never engage with — no existing tracks, not starred, no `@me`, no reports/peers in the discussion, no action items — does not produce tracks no matter how many digests it generates. The `scoreChannel` gate returns 0 and the channel is skipped before any AI call.

Scoring (additive, all must be visible to LLM only when score ≥ 1):
- channel has existing tracks: **+3**
- channel is starred in user profile: **+2**
- `@me` mention in `key_messages` or `situations`: **+2**
- a report/peer (per `user_profile.reports/peers`) in topic content: **+1**
- topic has non-empty `action_items`: **+1**

**Why locked:** Without this gate, every chatty channel (announcement feeds, deploy bots, off-topic) produces tracks because the LLM is willing to invent action items from any discussion. The gate is the difference between "tracks for me" and "everything that happened in Slack today" — it's also a major cost lever (skipped channels save a full LLM round-trip).

**Test guards:**
- `internal/tracks/pipeline_test.go::TestScoreChannel`

**Locked since:** 2026-04-28

## TRACKS-03 — "Watching" lane stays narrow

**Status:** Partial

**Observable:** Tracks I'm only watching (ownership=`watching`) are reserved for things that might actually need my eye — not background hum. Specifically `shouldDropTrack` filters them post-AI:
- `ownership=watching` + `priority=low` → always dropped.
- `ownership=watching` + `priority=medium` + `category ∈ {follow_up, discussion}` + empty `blocking` → dropped.
- Anything else (`ownership=mine|delegated`, or watching+high, or watching+medium with a blocking signal) → kept.

**Why locked:** The "watching" lane was added so managers/leads see decisions and blockers in their area without owning every line. Without the filter the LLM tends to widen "watching" into a firehose of every adjacent discussion, which collapses the lane back into noise and managers stop checking it. This contract is what keeps the manager use case viable.

**Tracked gap:** No direct unit test today — the rule lives only in `internal/tracks/pipeline.go::shouldDropTrack`. Integration tests cover storeTrackItems but don't pin the filter table. Need a focused `TestTracks03_WatchingLowAlwaysDropped` / `TestTracks03_WatchingMediumDiscussionDropped` / `TestTracks03_WatchingMediumWithBlockingKept` table test before this can be marked Enforced.

**Test guards (partial):**
- _(none yet — see Tracked gap)_

**Locked since:** 2026-04-28

## TRACKS-04 — Read once, stay read; re-surface only on real change

**Status:** Enforced

**Observable:** When I open a track:
- Its source digests are also marked read (cascade through `related_digest_ids`). I never see a digest light up as unread for content I already saw via its track.
- `has_updates` clears on read.

When the AI re-extracts a track I've already read and there's actually new content:
- `has_updates` flips back to `1` (only if the track was previously marked read — first-time updates on never-read tracks don't artificially flip the flag).

**Why locked:** This is the entire unread-tracking surface. Without the cascade, digests linger in the feed as unread forever after the user reads the surfaced track, and the badge counts diverge from reality. Without the conditional re-surface, every daemon cycle would either flip everything to "updated" (badge spam) or never flip anything (silent drift) — the read-aware re-surface is what makes the badge mean something.

**Test guards:**
- `internal/db/tracks_test.go::TestMarkTrackRead_CascadeDigests`
- `internal/db/tracks_test.go::TestUpsertTrack_Update`
- `internal/db/tracks_test.go::TestMarkTrackRead`
- `internal/db/tracks_test.go::TestSetTrackHasUpdates`
- `internal/tracks/pipeline_test.go::TestMarkTrackRead`

**Locked since:** 2026-04-28

## TRACKS-05 — AI cannot edit a track it doesn't own

**Status:** Partial

**Observable:** When the LLM returns `existing_id: N` to update a track, the pipeline checks `GetTrackAssignee(N) == current_user_id`. Mismatch (or missing track) → the `existing_id` is dropped, the item flows to the create-with-dedup path, and the existing track is untouched. A hallucinated or stale `existing_id` cannot corrupt another user's track or clobber an unrelated thread.

**Why locked:** The existing_id update path bypasses fingerprint/text dedup — it's a direct write. Without the owner gate, a single AI hallucination could rewrite the wrong track's text/priority/ownership, and the user would see "their" track suddenly become someone else's content. In multi-user setups (shared workspace DB, Desktop reading the same SQLite as the daemon) this is also a privacy boundary.

**Tracked gap:** Currently tested only by code-path inspection. Need explicit unit tests: `TestTracks05_ExistingIDOwnerMismatchFallsThroughToCreate` and `TestTracks05_ExistingIDMissingFallsThroughToCreate`. Also worth a test for the "owner matches → update succeeds" happy path with a different user as bystander, to pin the gate exactly.

**Test guards (partial):**
- _(none yet — see Tracked gap)_

**Locked since:** 2026-04-28

## TRACKS-06 — Re-extraction never narrows history

**Status:** Partial

**Observable — channel/digest origins (Enforced):** When a track is updated by extraction, its `channel_ids` and `related_digest_ids` arrays grow — they're merged with the new values placed first, deduped against existing values. A track that originally surfaced in `#backend` and later resurfaces in `#frontend` ends up with both channels recorded; the UI's "Open in Slack" picks the freshest channel (index 0 of merged), but the historical channel is still there for context. Same for digest IDs — the chain back to source content is never trimmed.

**Observable — track state history (Aspirational):** A track also preserves a record of its **own** past states — what its `text`, `context`, `priority`, `ownership`, and `category` looked like before the latest re-extraction overwrote them. The user can scroll back through how the AI's reading of the same situation evolved (the original "Review API PR" → later refined to "Review API PR + reconcile with auth team"); a bad re-extraction is recoverable instead of silently destructive; the LLM can be given the prior state on the next cycle so its updates feel continuous rather than amnesic.

**Why locked:** When a thread spans multiple channels (e.g. an incident discussed in `#incidents`, then post-mortemed in `#postmortems`), losing earlier channels on re-extraction would orphan the user from where the conversation actually started. The "Open in Slack" deep-link must remain accurate even when extraction re-runs. Replacing the array (instead of merging) would also retroactively rewrite history, which is hostile in a forensic tool. The same "no destructive rewrites" principle applies to the track's own fields: an LLM cycle should never silently obliterate the prior text the user may have already read or acted on. Without a state log the user has no way to answer "did this track always say that, or did the AI just rewrite it?", which collapses trust in the surface.

**Test guards (Enforced part):**
- `internal/db/tracks_test.go::TestUpdateTrackFromExtraction`
- `internal/db/tracks_test.go::TestMergeJSONArrays`

**Tracked gap (Aspirational part):** A `track_history` table existed pre-v43 and was dropped during the chains→tracks v3 refactor (`internal/db/db.go:1905`); it was never reinstated. Today `internal/db/tracks.go::UpdateTrackFromExtraction` overwrites `text`, `context`, `priority`, `ownership`, `category`, `decision_summary`, `participants`, `tags`, `sub_items`, etc. in place, and the prior values are unrecoverable. Closing this gap requires: (1) a new `track_states` (or revived `track_history`) table keyed by `track_id`, recording a snapshot of all narrative fields at each `UpdateTrackFromExtraction` call along with the `prompt_version` and `model` of the run that produced it; (2) `TrackDetailView` UI to expose the timeline of past states; (3) optionally, prior-state injection into the next extraction prompt so updates compose instead of replace; (4) a retention rule (e.g. last N states or N days) so the table doesn't grow unbounded. Until this lands, treat any non-trivial change to `UpdateTrackFromExtraction` as also rewriting an unobservable history — and ask the owner before doing so.

**Locked since:** 2026-04-28

## TRACKS-07 — Dismiss is final and excludes by default

**Status:** Partial

**Observable:** Dismissing a track removes it from every default list (`GetAllActiveTracks`, `GetTracks` without `IncludeDismissed`), from the Desktop tracks tab, and from the LLM prompt context (`formatExistingTracks` builds from `allActiveTracksRef`, which excludes dismissed). It also no longer appears in any cross-channel/dedup checks — once dismissed, the AI can rediscover the same situation as a fresh track only if the user explicitly restores it (`RestoreTrack`).

`dismissed_at` is the only "negative" state. There is no `done` / `resolved` / `archived` for tracks — dismiss is the single user-driven removal action.

**Why locked:** Dismiss is the user's "I've handled this / I don't care" signal. If dismissed tracks bled back into the prompt context, the AI would propose to recreate them on the next cycle and the dismiss action would feel broken. If they bled into the feed, the user loses the only available cleanup tool. Adding new "negative" statuses (`resolved`, `archived`, etc.) without explicit owner approval splits the cleanup surface and re-introduces the inbox-zero pressure tracks were designed to avoid.

**Tracked gap:** No explicit "dismissed-not-shown" test. Existing tests cover the active path but never assert that a dismissed track is excluded from `GetAllActiveTracks`, `formatExistingTracks`, or the dedup helpers (`FindSimilarTrack`, `FindTracksByFingerprint`). Need: `TestTracks07_DismissedExcludedFromActiveList`, `TestTracks07_DismissedNotInPromptContext`, `TestTracks07_DismissedDoesNotBlockRediscovery`.

**Test guards (partial):**
- `internal/db/tracks_test.go::TestGetAllActiveTracks` (implicit — only inserts active rows)
- `internal/db/tracks_test.go::TestGetTracks_Filters` (implicit — `IncludeDismissed` defaults to false)

**Locked since:** 2026-04-28

## Changelog

- 2026-04-28: file created with 7 contracts (TRACKS-01..07). Three are Enforced (01, 02, 04), four are Partial with explicit tracked gaps (03 watching-lane filter has no unit test, 05 cross-user gate has no unit test, 06 channel/digest part Enforced but per-track state history Aspirational, 07 dismissed-exclusion has only implicit tests). Existing tests are referenced under their current names; renaming to `TestTracks0N_…` convention is a follow-up so the four soft-protection layers all engage.
- 2026-04-28: TRACKS-06 expanded — added "track state history" as Aspirational sub-contract. The pre-v43 `track_history` table was dropped during chains→tracks v3 refactor; `UpdateTrackFromExtraction` currently overwrites narrative fields in place. Status demoted Enforced → Partial; channel/digest merge guarantees remain fully Enforced under the same ID.
- 2026-04-28: added preamble section "What a track is and what it's built from" — defines the narrative-row-not-task surface, lists the upstream signals the pipeline consumes (`digests.situations`, `digests.topics`, channel `running_summary`, existing tracks, `user_profile`, cross-cycle `source_refs` marker), and frames the seven contracts as a single trust property: the count must mean something or the surface collapses.
