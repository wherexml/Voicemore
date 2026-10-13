//go:build darwin

package macos

import (
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

// Capture is a running recording of the default input device.
type Capture struct {
	q       uintptr
	onPCM   func(pcm []byte)
	stopped atomic.Bool
	output  *outputSilencer
}

// StartCapture records the default microphone and gives onPCM the samples,
// about 20 ms at a time, on a thread of the system's: it must not block.
// One capture runs at a time.
func StartCapture(onPCM func(pcm []byte)) (*Capture, error) {
	captureLifecycle.Lock()
	defer captureLifecycle.Unlock()
	if err := loadAudioToolbox(); err != nil {
		return nil, err
	}
	if captureCurrent.Load() != nil {
		return nil, fmt.Errorf("the microphone is already recording")
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
	if st := audioQueueNewInput(&desc, captureCallback, 0, 0, 0, 0, &c.q); st != 0 {
		return nil, fmt.Errorf("the microphone could not be opened (AudioQueueNewInput, OSStatus %d)", st)
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
	output, err := silenceOutput()
	if err != nil {
		c.stopLocked()
		return nil, err
	}
	c.output = output
	if st := audioQueueStart(c.q, 0); st != 0 {
		c.stopLocked()
		return nil, fmt.Errorf("the microphone did not start (AudioQueueStart, OSStatus %d); check the Microphone permission and the input device", st)
	}
	return c, nil
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
