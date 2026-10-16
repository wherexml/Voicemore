//go:build darwin

package macos

import (
	"log"
	"time"
	"unsafe"
)

// Read before opening the microphone: Bluetooth may keep different playback
// gain and format while its microphone is in use.
type bluetoothOutput struct {
	device  uint32
	rate    float64
	volumes map[uint32]float32
	muted   uint32
	hasMute bool
}

func readAudioValue[T any](device, selector, scope uint32, value *T) bool {
	addr := audioPropertyAddress{selector, scope, 0}
	size := uint32(unsafe.Sizeof(*value))
	return outputGet(device, &addr, 0, 0, &size, unsafe.Pointer(value)) == 0
}

func snapshotBluetoothOutput(input uint32) *bluetoothOutput {
	if loadOutputAudio() != nil {
		return nil
	}
	inputTransport, err := readOutputProperty(input, audioPropertyAddress{0x7472616e, scopeGlob, 0})
	if err != nil || (inputTransport != 0x626c7565 && inputTransport != 0x626c6561) {
		return nil
	}
	device, err := readOutputProperty(1, audioPropertyAddress{0x644f7574, scopeGlob, 0})
	if err != nil {
		return nil
	}
	transport, err := readOutputProperty(device, audioPropertyAddress{0x7472616e, scopeGlob, 0}) // tran
	if err != nil || (transport != 0x626c7565 && transport != 0x626c6561) {
		return nil
	} // blue, blea
	b := &bluetoothOutput{device: device, volumes: map[uint32]float32{}}
	b.muted, err = readOutputProperty(device, outputMuteAddress)
	b.hasMute = err == nil
	readAudioValue(device, 0x6e737274, scopeGlob, &b.rate) // nsrt
	for _, channel := range []uint32{0, 1, 2} {
		var volume float32
		addr := audioPropertyAddress{0x766f6c6d, 0x6f757470, channel}
		size := uint32(4)
		if outputGet(device, &addr, 0, 0, &size, unsafe.Pointer(&volume)) == 0 {
			b.volumes[channel] = volume
		}
	}
	return b
}

// Keep the existing mute while CoreAudio settles. This is bounded: another
// application may legitimately keep the headset microphone open.
func (b *bluetoothOutput) settle(input uint32, keepMuted bool) {
	if b == nil {
		return
	}
	start := time.Now()
	stable := time.Time{}
	for time.Since(start) < 8*time.Second {
		if keepMuted {
			b.keepMuted()
		}
		b.limitVolume()
		var rate float64
		var running uint32
		rateOK := readAudioValue(b.device, 0x6e737274, scopeGlob, &rate)
		inputOK := readAudioValue(input, 0x676f6e65, scopeGlob, &running) // gone
		if !rateOK {
			log.Printf("Bluetooth output disappeared before restore")
			return
		}
		if inputOK && running == 0 && (b.rate == 0 || rate == b.rate) {
			if stable.IsZero() {
				stable = time.Now()
			}
			if time.Since(start) >= 600*time.Millisecond && time.Since(stable) >= 300*time.Millisecond {
				return
			}
		} else {
			stable = time.Time{}
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Printf("Bluetooth output recovery timed out; playback format or microphone still active")
}

// Never raise gain, and never change a newly selected output. The pre-capture
// level is a safety ceiling, including when a profile switch raises the gain.
func (b *bluetoothOutput) limitVolume() {
	if b == nil {
		return
	}
	current, err := readOutputProperty(1, audioPropertyAddress{0x644f7574, scopeGlob, 0})
	if err != nil || current != b.device {
		return
	}
	fallback := float32(1)
	for _, value := range b.volumes {
		if value < fallback {
			fallback = value
		}
	}
	if len(b.volumes) == 0 {
		return
	}
	for _, channel := range []uint32{0, 1, 2} {
		ceiling, exists := b.volumes[channel]
		if !exists {
			ceiling = fallback
		}
		var volume float32
		addr := audioPropertyAddress{0x766f6c6d, 0x6f757470, channel}
		size := uint32(4)
		if outputGet(b.device, &addr, 0, 0, &size, unsafe.Pointer(&volume)) != 0 || volume <= ceiling {
			continue
		}
		if st := outputSet(b.device, &addr, 0, 0, 4, unsafe.Pointer(&ceiling)); st != 0 {
			log.Printf("Bluetooth output volume restore failed: OSStatus %d", st)
		}
	}
}

func (b *bluetoothOutput) keepMuted() {
	if b == nil {
		return
	}
	if muted, err := readOutputProperty(b.device, outputMuteAddress); err == nil && muted == 0 {
		if err := setOutputMute(b.device, 1); err != nil {
			log.Printf("Bluetooth transition mute: %v", err)
		}
	}
}
