package asr

import "time"

// ProcessedResult is the receipt from an integrated ASR + cleanup service.
// Text is the billed cleaned or translated receipt. Ordinary cleanup-off
// delivery uses RawText; explicit translation always uses Text.
type ProcessedResult struct {
	TranslationLanguage string    `json:"translation_language,omitempty"`
	ASRFinalAt          time.Time `json:"-"`
	ID                  string    `json:"id"`
	Model               string    `json:"model"`
	Text                string    `json:"text"`
	RawText             string    `json:"raw_text"`
	BillableCharacters  int       `json:"billable_characters"`
	Currency            string    `json:"currency"`
	ChargeUSD           string    `json:"charge_usd"`
	PriceVersion        string    `json:"price_version"`
	UsageLogID          int64     `json:"usage_log_id"`
	Timings             struct {
		Cleanup           int64 `json:"cleanup"`
		ASRAfterLastAudio int64 `json:"asr_after_last_audio"`
	} `json:"timings_ms"`
}

type ProcessedStream interface {
	Stream
	Processed() *ProcessedResult
}
