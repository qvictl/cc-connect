package core

import "strings"

// narrationProgress is [display] narration = "progress": the text an agent
// writes on the way to its answer -- before a tool call, a thinking block or
// a permission prompt -- goes into the turn's card or compact progress message as an update
// instead of into messages of its own, and the text is not streamed as a
// preview, since until the turn ends nobody knows which segment is the
// answer. The answer is the one message a turn sends; when the turn ends on
// a tool call with no text after it, the last narration is the answer. A
// platform without an updatable progress target gets the narration as messages, as with
// "message".
const narrationProgress = "progress"

// narrationInProgress reports whether narration goes into the progress card.
func (e *Engine) narrationInProgress() bool {
	return e.display.Narration == narrationProgress && !e.finalOutput()
}

// narrate puts one finished narration segment into the progress card and
// reports whether it did; false means the caller sends it as a message.
// A blank segment or a bare NO_REPLY is handled by showing nothing.
func narrate(cp *compactProgressWriter, segment string) bool {
	segment = strings.TrimSpace(segment)
	if segment == "" || isSilentReply(segment) {
		return true
	}
	if cp == nil || !cp.enabled || cp.failed || (cp.style != progressStyleCard && cp.style != progressStyleCompact) {
		return false
	}
	if !cp.AppendStructured(ProgressCardEntry{Kind: ProgressEntryInfo, Text: segment}, segment) {
		return false
	}
	// Buffered/throttled updates and failed edits are not delivery. Preserve
	// narration in a standalone fallback rather than relying on a later edit.
	return cp.content == cp.lastSent
}

// A terminal result is authoritative, including NO_REPLY. Only absent result
// text can fall back to the last completed narration segment.
func narrationAnswer(result, tail, last string) string {
	if strings.TrimSpace(result) != "" {
		return result
	}
	if strings.TrimSpace(tail) != "" {
		return tail
	}
	return last
}
