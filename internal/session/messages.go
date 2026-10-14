package session

import (
	"errors"
	"fmt"
)

// Messages the controller shows or stores. The app gives Controller.Tr to
// have them in the user's language; without it they are these, in English.
var messages = map[string]string{
	"micDenied":       "Microphone access is denied: allow Voicemore in System Settings → Privacy & Security → Microphone.",
	"micNotGranted":   "Microphone access was not granted, so nothing can be recorded.",
	"micMissing":      "No microphone found. Connect one and try again.",
	"micNoFrames":     "The microphone did not deliver audio. Check the input device and try again.",
	"endedEarly":      "The recording ended before it started.",
	"asrBusy":         "The speech recognition service is busy with another recording. Try again in a moment.",
	"asrUnreachable":  "The speech recognition service at %s cannot be reached: %s",
	"noSpeech":        "No speech detected — nothing inserted.",
	"cleanupFailed":   "Cleanup failed: %s",
	"notInserted":     "Not inserted: %s",
	"detailPractice":  "practice in the setup guide; not inserted into any app",
	"copiedFallback":  "Text saved to clipboard — press Cmd+V to paste.",
	"hotwordNotSent":  "The dictionary was not sent to the recognizer; its default terms were used: %s",
	"hotwordEmpty":    "The local dictionary is empty; the recognizer's default terms were used.",
	"hotwordSent":     "Sent %d dictionary terms to the recognizer.",
	"hotwordDefault":  "recognizer's default terms",
	"hotwordCloud":    "The dictionary's term hints are not sent to cloud recognizers.",
	"hotwordLocal":    "%d dictionary terms",
	"noteRules":       "rules: %s",
	"noteIncomplete":  "incomplete: %s",
	"observeNoRead":   "Edits after insertion could not be observed: the target app does not let this input be read back. Revise in History instead.",
	"observeOutside":  "Observation ended: the input changed outside the inserted text; nothing was learned.",
	"observeSame":     "Observation ended: the inserted text was not changed.",
	"observeLearned":  "An edit after insertion was observed: %s",
	"revisionSame":    "The revision is the same as the original.",
	"recordGone":      "The record no longer exists.",
	"noAudio":         "This record has no audio to use.",
	"busy":            "A recording or transcription is in progress. Try again in a moment.",
	"detailPasted":    "pasted into %s",
	"detailNoElement": "the app exposed no focused element, so only the app could be checked",
	"detailRemote":    "paste shortcut sent to %s; remote insertion is unverified; clipboard kept for synchronization",
	"detailClipboard": "the clipboard changed meanwhile and was left as it is",
	"detailCancelled": "cancelled before insertion",
	"detailNoSpeech":  "the recognizer returned no speech",
	"detailProgress":  "in progress",
	"detailCleanup":   "cleanup failed; the transcript was not inserted yet",
	"detailAudio":     "the audio could not be saved: %s",
}

func (c *Controller) tr(key string, args ...any) string {
	if c.Tr != nil {
		return c.Tr(key, args...)
	}
	if len(args) == 0 {
		return messages[key]
	}
	return fmt.Sprintf(messages[key], args...)
}

// explain puts an error of a lower layer in words for the user.
func (c *Controller) explain(err error) string {
	if c.Explain != nil {
		return c.Explain(err)
	}
	return err.Error()
}

// cause is the innermost error's text: "connection refused" rather than
// the whole chain.
func cause(err error) string {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err.Error()
		}
		err = next
	}
}
