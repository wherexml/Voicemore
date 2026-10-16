package main

import (
	"encoding/binary"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"math"
	"time"
	"voiceinput/internal/store"
)

// Load only the selected recording, off the UI thread. Peaks are PCM16 amplitudes.
func (m *mainWindow) loadWave(r store.Record) {
	key := r.ID + "/" + r.AudioFile
	if m.waveID == key {
		return
	}
	m.waveID = key
	m.wavePeaks = nil
	m.waveLoading = true
	go func() {
		pcm, err := m.a.history.ReadPCM(r.AudioFile)
		var peaks []float32
		if err == nil && len(pcm) >= 2 {
			peaks = make([]float32, 88)
			samples := len(pcm) / 2
			for i := range peaks {
				from, to := i*samples/88, (i+1)*samples/88
				for j := from; j < to; j++ {
					v := float32(math.Abs(float64(int16(binary.LittleEndian.Uint16(pcm[j*2:]))))) / 32768
					if v > peaks[i] {
						peaks[i] = v
					}
				}
			}
		}
		m.win.Update(func() {
			if m.waveID == key {
				m.wavePeaks = peaks
				m.waveLoading = false
			}
		})
	}()
}
func (m *mainWindow) recordDetail(c *ui.Context, r store.Record) {
	t := c.Theme()
	ui.Row(c.Key("player")).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
		if !r.HasAudio() {
			key := "noAudioOff"
			if r.AudioRemoved != "" {
				ui.Text(c, T("noAudioGone", r.AudioRemoved)).FontSize(12).TextColor(t.TextMuted)
			} else {
				ui.Text(c, T(key)).FontSize(12).TextColor(t.TextMuted)
			}
			return
		}
		m.loadWave(r)
		playing := m.player != nil && m.playerID == r.ID && m.player.Playing()
		glyph, label := icPlay, T("play")
		if playing {
			glyph, label = icPause, T("pause")
		}
		if ui.ButtonBase(c.Key("play")).Padding(0).AlignItems(ui.Center).Justify(ui.Center).Label(label).Tooltip(label).Children(func() { ui.Icon(c, glyph).Size(12, 12).TextColor(softText(t)) }).Size(30, 28).Radius(7).Background(cardColor(t)).Border(.5, settingsGroupLine(c)).Clicked() {
			if err := m.openPlayer(r); err != nil {
				m.say("cantPlay", err.Error())
			} else if playing {
				m.player.Pause()
			} else {
				m.player.Play()
			}
		}
		total := math.Max(float64(r.DurationMs)/1000, .1)
		if m.player != nil && m.playerID == r.ID {
			m.position = m.player.Position().Seconds()
			if playing {
				c.After(60 * time.Millisecond)
			}
		}
		seek := ui.SliderBase(c.Key("seek"), &m.position, 0, total).Grow(1).MinWidth(0).Height(34).Label(T("seek"))
		seek.Draw(func(p *ui.Painter, rect ui.Rect) {
			if len(m.wavePeaks) == 0 {
				p.Fill(ui.Rect{X: rect.X, Y: rect.Y + 16, W: rect.W, H: 2}, settingsGroupLine(c), 1)
				return
			}
			for i, v := range m.wavePeaks {
				h := float32(3) + float32(math.Sqrt(float64(v)))*29
				x := rect.X + float32(i)*rect.W/88
				color := pick(t, "#cccccc", "#4d4d4d")
				if float64(i)/88 < m.position/total {
					color = t.Accent
				}
				p.Fill(ui.Rect{X: x, Y: rect.Y + (rect.H-h)/2, W: max(1, rect.W/88-3), H: h}, color, 1.5)
			}
		})
		if seek.Changed() {
			if err := m.openPlayer(r); err == nil {
				m.player.Seek(time.Duration(m.position * float64(time.Second)))
			}
		}
		ui.Text(c, clock(int64(m.position*1000))+" / "+clock(r.DurationMs)).FontSize(11).Font("Menlo").TextColor(t.TextMuted).SingleLine()
	})
	ui.Box(c).FillWidth().Height(.5).Background(settingsGroupLine(c))
	ui.Row(c).FillWidth().Wrap().Gap(16).Children(func() {
		ui.Text(c, T("v2Duration")+" "+duration(r.DurationMs)).FontSize(11.5).TextColor(t.TextMuted)
		asr := r.ASRModel
		if asr == "" {
			asr = "—"
		}
		ui.Text(c, T("mAsr")+" "+asr).FontSize(11.5).TextColor(t.TextMuted)
		cleanup := r.CleanupModel
		if !r.CleanupRequested {
			cleanup = T("off")
		} else if cleanup == "" {
			cleanup = "—"
		}
		ui.Text(c, T("cleanup")+" "+cleanup).FontSize(11.5).TextColor(t.TextMuted)
		statusTag(c, r)
	})
	ui.Box(c).FillWidth().Height(.5).Background(settingsGroupLine(c))
	ui.Grid(c).Columns(4).FillWidth().Gap(8).Children(func() {
		vals := []int64{r.Timing.FirstPartialMs, r.Timing.StopToFinalMs, r.Timing.CleanupMs, r.Timing.FinalToInsertMs}
		for i, key := range []string{"v3First", "v3Final", "cleanup", "v3Insert"} {
			value := fmt.Sprintf("%d ms", vals[i])
			failed := i == 2 && r.CleanupError != ""
			if vals[i] <= 0 || (i == 2 && !r.CleanupRequested) || (i == 3 && r.Status != store.StatusInserted) {
				value = "—"
			}
			if failed {
				value = T("failedTag")
			}
			ui.Column(c.Key(i)).Gap(5).Children(func() {
				ui.Text(c, T(key)).FontSize(11).TextColor(t.TextMuted)
				v := ui.Text(c, value).FontSize(12).Font("Menlo")
				if failed {
					v.TextColor(t.Danger)
				}
			})
		}
	})
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c.Key("reclean"), T("reclean")).Height(28).FontSize(12).Disabled(r.Transcript() == "" || m.busy != nil).Clicked() {
			m.do("recleaning", func() error { return m.a.ctrl.Recleanup(r.ID, r.Transcript()) }, "versionAdded")
		}
		label := T("v3CopyRaw")
		if at, ok := m.recentCopied["raw-"+r.ID]; ok && time.Since(at) < 1500*time.Millisecond {
			label = T("copied")
			c.After(1500*time.Millisecond - time.Since(at))
		}
		if ui.Button(c.Key("copy-raw"), label).Height(28).FontSize(12).Disabled(r.RawText == "").Clicked() {
			mygo.Clipboard.WriteText(r.RawText)
			if m.recentCopied == nil {
				m.recentCopied = map[string]time.Time{}
			}
			m.recentCopied["raw-"+r.ID] = time.Now()
		}
	})
}
