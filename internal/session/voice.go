package session

import (
	"context"
	"errors"
	"time"

	"voiceinput/internal/asr"
	"voiceinput/internal/store"
)

func voiceBilling(p *asr.ProcessedResult) *store.VoiceBilling {
	return &store.VoiceBilling{BillableCharacters: p.BillableCharacters, Currency: p.Currency, ChargeUSD: p.ChargeUSD, PriceVersion: p.PriceVersion, UsageLogID: p.UsageLogID}
}

// RecoverVoiceResult retrieves the original task after a lost response. It
// updates history for copying; it never records, charges or auto-pastes.
func (c *Controller) RecoverVoiceResult(id string) error {
	r, ok := c.History.Get(id)
	if !ok || r.VoiceTaskID == "" {
		return errors.New("no cloud task to recover")
	}
	d, err := c.taskDialer(c.Settings.Get(), r.TranslationLanguage)
	if err != nil {
		return err
	}
	api, ok := d.(interface {
		Result(context.Context, string) (*asr.ProcessedResult, error)
	})
	if !ok {
		return errors.New("select the original sub2api service to recover this task")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, err := api.Result(ctx, r.VoiceTaskID)
	if err != nil {
		return err
	}
	ok, err = c.History.Update(id, func(rec *store.Record) {
		if rec.VoiceBilling != nil {
			return
		}
		rec.RawText, rec.CleanedText, rec.CleanupModel = p.RawText, p.Text, "deepseek-flash"
		rec.Corrected, rec.CorrectedText, rec.AppliedRules = false, "", nil
		rec.VoiceBilling = voiceBilling(p)
		rec.CleanupError = ""
		rec.Timing.CleanupMs = p.Timings.Cleanup
		rec.Status, rec.StatusDetail = store.StatusNotInserted, ""
	})
	c.historyChanged()
	if err == nil && !ok {
		return errors.New(c.tr("recordGone"))
	}
	return err
}
