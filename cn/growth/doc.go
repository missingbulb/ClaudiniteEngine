// Package growth is the engine's half of the growth lifecycle: what a
// session leaves behind and the tools the growth skills name. capture
// pushes a session's transcript, scrubbed, onto the conversation-logs
// branch as a session-keyed delta (cn growth capture, and the
// session-end hook); prune applies the repo's retention to that branch
// (cn growth prune, the logs-prune task's code-work); provenance is the
// member's provenance verbs over shared/provenance (cn provenance);
// scaffold creates and declares the local pack a lesson lands in
// (cn pack new). The extract, dedup and review tasks that turn
// captures into pack content are pack tasks the runner executes.
package growth
