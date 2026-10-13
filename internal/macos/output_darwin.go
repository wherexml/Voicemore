//go:build darwin

package macos

import (
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

type audioPropertyAddress struct{ selector, scope, element uint32 }

var (
	outputLoad sync.Once
	outputErr  error
	outputGet  func(uint32, *audioPropertyAddress, uint32, uintptr, *uint32, unsafe.Pointer) int32
	outputSet  func(uint32, *audioPropertyAddress, uint32, uintptr, uint32, unsafe.Pointer) int32
)

func loadOutputAudio() error {
	outputLoad.Do(func() {
		lib, err := purego.Dlopen(frameworks+"CoreAudio.framework/CoreAudio", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			outputErr = err
			return
		}
		purego.RegisterLibFunc(&outputGet, lib, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&outputSet, lib, "AudioObjectSetPropertyData")
	})
	return outputErr
}

func readOutputProperty(device uint32, addr audioPropertyAddress) (uint32, error) {
	var value uint32
	size := uint32(4)
	if st := outputGet(device, &addr, 0, 0, &size, unsafe.Pointer(&value)); st != 0 {
		return 0, fmt.Errorf("read audio output property: OSStatus %d", st)
	}
	return value, nil
}

var outputMuteAddress = audioPropertyAddress{0x6d757465, 0x6f757470, 0} // mute, outp, main

func setOutputMute(device, value uint32) error {
	addr := outputMuteAddress
	if st := outputSet(device, &addr, 0, 0, 4, unsafe.Pointer(&value)); st != 0 {
		return fmt.Errorf("set audio output mute: OSStatus %d", st)
	}
	// Device property changes can be asynchronous. Do not open the microphone
	// until the hardware reports that it has applied the mute.
	for range 50 {
		actual, err := readOutputProperty(device, addr)
		if err != nil {
			return err
		}
		if actual == value {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("audio output did not apply mute state")
}

type outputSilencer struct{ device, previous uint32 }

func silenceOutput() (*outputSilencer, error) {
	if err := loadOutputAudio(); err != nil {
		return nil, err
	}
	device, err := readOutputProperty(1, audioPropertyAddress{0x644f7574, 0x676c6f62, 0}) // dOut, glob
	if err != nil {
		return nil, err
	}
	previous, err := readOutputProperty(device, outputMuteAddress)
	if err != nil {
		return nil, fmt.Errorf("无法自动静音当前音频输出，请切换到支持静音的输出设备：%w", err)
	}
	s := &outputSilencer{device: device, previous: previous}
	if previous == 0 {
		if err := setOutputMute(device, 1); err != nil {
			s.restore()
			return nil, fmt.Errorf("无法自动静音，录音未开始：%w", err)
		}
	}
	return s, nil
}

func (s *outputSilencer) restore() {
	if s == nil || s.previous != 0 {
		return
	}
	// Restore the same device, even if the user selected another output meanwhile.
	// The volume scalar is never touched.
	if err := setOutputMute(s.device, s.previous); err != nil {
		log.Printf("restore recording output: %v", err)
	}
}
