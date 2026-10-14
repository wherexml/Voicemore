//go:build darwin

package macos

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// The microphone is read through an Audio Queue, which converts the input
// device's format to what the ASR takes: PCM16, mono, 16 kHz.

type audioStreamBasicDescription struct {
	sampleRate       float64
	formatID         uint32
	formatFlags      uint32
	bytesPerPacket   uint32
	framesPerPacket  uint32
	bytesPerFrame    uint32
	channelsPerFrame uint32
	bitsPerChannel   uint32
	reserved         uint32
}

type audioQueueBuffer struct {
	audioDataBytesCapacity uint32
	audioData              unsafe.Pointer
	audioDataByteSize      uint32
	userData               uintptr
	packetDescCapacity     uint32
	packetDescs            uintptr
	packetDescCount        uint32
}

const (
	audioFormatLinearPCM     = 0x6C70636D // 'lpcm'
	audioFlagIsSignedInteger = 1 << 2
	audioFlagIsPacked        = 1 << 3
	captureBuffers           = 4
	captureBufferBytes       = 640 // 20 ms: prompt onset feedback and smaller stop tail
)

var (
	aqLoad sync.Once
	aqErr  error

	audioQueueNewInput       func(format *audioStreamBasicDescription, callback, user, runLoop, runLoopMode uintptr, flags uint32, out *uintptr) int32
	audioQueueAllocateBuffer func(q uintptr, size uint32, out **audioQueueBuffer) int32
	audioQueueEnqueueBuffer  func(q uintptr, b *audioQueueBuffer, n uint32, descs uintptr) int32
	audioQueueStart          func(q uintptr, at uintptr) int32
	audioQueueStop           func(q uintptr, immediate bool) int32
	audioQueueDispose        func(q uintptr, immediate bool) int32
	audioQueueSetProperty    func(q uintptr, id uint32, data unsafe.Pointer, size uint32) int32

	captureLifecycle sync.Mutex
	captureCallback  uintptr
	captureCurrent   atomic.Pointer[Capture]
)

func loadAudioToolbox() error {
	aqLoad.Do(func() {
		lib, err := purego.Dlopen(frameworks+"AudioToolbox.framework/AudioToolbox", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			aqErr = err
			return
		}
		purego.RegisterLibFunc(&audioQueueNewInput, lib, "AudioQueueNewInput")
		purego.RegisterLibFunc(&audioQueueAllocateBuffer, lib, "AudioQueueAllocateBuffer")
		purego.RegisterLibFunc(&audioQueueEnqueueBuffer, lib, "AudioQueueEnqueueBuffer")
		purego.RegisterLibFunc(&audioQueueStart, lib, "AudioQueueStart")
		purego.RegisterLibFunc(&audioQueueStop, lib, "AudioQueueStop")
		purego.RegisterLibFunc(&audioQueueDispose, lib, "AudioQueueDispose")
		purego.RegisterLibFunc(&audioQueueSetProperty, lib, "AudioQueueSetProperty")
		// The queue's own thread calls it with each filled buffer.
		captureCallback = purego.NewCallback(func(user, q uintptr, b *audioQueueBuffer, start, packets, descs uintptr) {
			c := captureCurrent.Load()
			if c == nil || c.q != q || c.stopped.Load() {
				return
			}
			if n := int(b.audioDataByteSize); n > 0 {
				pcm := make([]byte, n)
				copy(pcm, unsafe.Slice((*byte)(b.audioData), n))
				c.onPCM(pcm)
			}
			audioQueueEnqueueBuffer(q, b, 0, 0)
		})
	})
	return aqErr
}

// ErrNoMicrophone says there is nothing to record from: no input device is
// connected, or the one that was selected is gone.
var ErrNoMicrophone = errors.New("no microphone was found")

// InputDeviceMissing reports that the system has no default input device.
// When the system does not answer, it reports false: the capture's own
// error then says what went wrong.
func InputDeviceMissing() bool {
	device, ok := defaultInputDevice()
	return ok && device == 0
}

// defaultInputDevice is the system's default input device, 0 when there
// is none; ok is false when the system did not answer.
func defaultInputDevice() (device uint32, ok bool) {
	if loadOutputAudio() != nil {
		return 0, false
	}
	size := uint32(4)
	addr := audioPropertyAddress{0x64496e20, 0x676c6f62, 0} // dIn , glob
	st := outputGet(1, &addr, 0, 0, &size, unsafe.Pointer(&device))
	return device, st == 0
}

// DeviceGone reports that the device this recording started on is no
// longer there: unplugged, or switched off. It reads one property of the
// device and is cheap enough to ask a few times a second. A capture that
// was stopped reports false.
func (c *Capture) DeviceGone() bool {
	if c == nil || c.stopped.Load() || loadOutputAudio() != nil {
		return false
	}
	if c.device == 0 {
		// The device was not known at the start: all there is to ask is
		// whether the system has one at all.
		return InputDeviceMissing()
	}
	var alive uint32
	size := uint32(4)
	addr := audioPropertyAddress{0x6c69766e, 0x676c6f62, 0} // livn, glob
	// A removed device is no object any more: the read itself fails.
	return outputGet(c.device, &addr, 0, 0, &size, unsafe.Pointer(&alive)) != 0 || alive == 0
}

// captureError names a failure to open or start the queue: without an
// input device it is ErrNoMicrophone, whatever status the queue gave.
func captureError(format string, st int32) error {
	if InputDeviceMissing() {
		return fmt.Errorf("%w (OSStatus %d)", ErrNoMicrophone, st)
	}
	return fmt.Errorf(format, st)
}

// Capture is a running recording of the chosen or the default input device.
type Capture struct {
	q       uintptr
	device  uint32 // the device recorded from when the recording started, 0 if unknown
	onPCM   func(pcm []byte)
	stopped atomic.Bool
	output  *outputSilencer
}

// StartCapture records the default microphone and gives onPCM the samples,
// about 20 ms at a time, on a thread of the system's: it must not block.
// One capture runs at a time.
func StartCapture(onPCM func(pcm []byte)) (*Capture, error) {
	return StartCaptureFrom("", onPCM)
}

// StartCaptureFrom records the input device with this UID (InputDevices),
// or the system default when uid is empty or that device is not connected:
// an unplugged microphone does not stop dictation, and is used again once
// it is back.
func StartCaptureFrom(uid string, onPCM func(pcm []byte)) (*Capture, error) {
	captureLifecycle.Lock()
	defer captureLifecycle.Unlock()
	if err := loadAudioToolbox(); err != nil {
		return nil, err
	}
	if captureCurrent.Load() != nil {
		return nil, fmt.Errorf("the microphone is already recording")
	}
	chosen := inputDeviceByUID(uid) != 0
	if !chosen && InputDeviceMissing() {
		return nil, ErrNoMicrophone
	}
	desc := audioStreamBasicDescription{
		sampleRate:       16000,
		formatID:         audioFormatLinearPCM,
		formatFlags:      audioFlagIsSignedInteger | audioFlagIsPacked,
		bytesPerPacket:   2,
		framesPerPacket:  1,
		bytesPerFrame:    2,
		channelsPerFrame: 1,
		bitsPerChannel:   16,
	}
	c := &Capture{onPCM: onPCM}
	c.device, _ = defaultInputDevice()
	if st := audioQueueNewInput(&desc, captureCallback, 0, 0, 0, 0, &c.q); st != 0 {
		return nil, captureError("the microphone could not be opened (AudioQueueNewInput, OSStatus %d)", st)
	}
	if chosen {
		withPool(func() {
			if device := routeQueue(c.q, uid); device != 0 {
				c.device = device
			}
		})
	}
	captureCurrent.Store(c)
	for range captureBuffers {
		var b *audioQueueBuffer
		if st := audioQueueAllocateBuffer(c.q, captureBufferBytes, &b); st != 0 {
			c.stopLocked()
			return nil, fmt.Errorf("the microphone could not be opened (AudioQueueAllocateBuffer, OSStatus %d)", st)
		}
		audioQueueEnqueueBuffer(c.q, b, 0, 0)
	}
	// Output is muted by SilenceOutput after the readiness cue finishes.
	if st := audioQueueStart(c.q, 0); st != 0 {
		c.stopLocked()
		return nil, captureError("the microphone did not start (AudioQueueStart, OSStatus %d); check the Microphone permission and the input device", st)
	}
	return c, nil
}

// SilenceOutput runs after the start cue, serialized with Stop so an early
// second Fn press cannot leave the output muted after recording has ended.
func (c *Capture) SilenceOutput() error {
	captureLifecycle.Lock()
	defer captureLifecycle.Unlock()
	if c == nil || c.stopped.Load() || c.output != nil {
		return nil
	}
	output, err := silenceOutput()
	if err != nil {
		return err
	}
	c.output = output
	return nil
}

// Stop ends the recording. The samples the queue still holds are dropped.
func (c *Capture) Stop() {
	captureLifecycle.Lock()
	defer captureLifecycle.Unlock()
	c.stopLocked()
}

func (c *Capture) stopLocked() {
	if c == nil || c.stopped.Swap(true) {
		return
	}
	audioQueueStop(c.q, true)
	audioQueueDispose(c.q, true)
	c.output.restore()
	captureCurrent.CompareAndSwap(c, nil)
}

// MicrophoneStatus is "authorized", "denied", "restricted" or
// "undetermined", as AVFoundation reports it.
func MicrophoneStatus() string {
	if load() != nil {
		return "unknown"
	}
	status := 0
	OnMain(func() {
		status = objc.Send[int](class("AVCaptureDevice"), sel("authorizationStatusForMediaType:"), nsString("soun"))
	})
	switch status {
	case 1:
		return "restricted"
	case 2:
		return "denied"
	case 3:
		return "authorized"
	}
	return "undetermined"
}

// RequestMicrophone shows the system's prompt when the user has not
// answered it yet, and calls done with the answer, on another thread.
func RequestMicrophone(done func(granted bool)) {
	if load() != nil {
		done(false)
		return
	}
	OnMain(func() {
		block := objc.NewBlock(func(_ objc.Block, granted bool) { go done(granted) })
		class("AVCaptureDevice").Send(sel("requestAccessForMediaType:completionHandler:"), nsString("soun"), block)
	})
}
