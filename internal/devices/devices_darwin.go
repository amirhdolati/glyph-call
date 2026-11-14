//go:build darwin && cgo

package devices

/*
#cgo CFLAGS: -fobjc-arc -mmacosx-version-min=14.0 -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Foundation -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework AudioToolbox
#include "native.h"
*/
import "C"

import (
	"errors"
	"image"
	"unsafe"
)

func Supported() bool { return true }
func Cameras() []string {
	names := []string{}
	for i := 0; i < int(C.ac_camera_count()); i++ {
		names = append(names, C.GoString(C.ac_camera_name(C.int(i))))
	}
	return names
}

type Camera struct {
	raw    []byte
	closed bool
}

func OpenCamera(index int) (*Camera, error) {
	var message [1024]C.char
	if C.ac_camera_start(C.int(index), &message[0], 1024) == 0 {
		return nil, errors.New(C.GoString(&message[0]))
	}
	return &Camera{raw: make([]byte, 1920*1080*4)}, nil
}
func (c *Camera) Read() (image.Image, error) {
	if c.closed {
		return nil, errors.New("camera closed")
	}
	var w, h, stride C.int
	n := C.ac_camera_read(unsafe.Pointer(&c.raw[0]), C.int(len(c.raw)), &w, &h, &stride)
	if n == 0 {
		return nil, nil
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for y := 0; y < int(h); y++ {
		for x := 0; x < int(w); x++ {
			s := y*int(stride) + x*4
			d := y*img.Stride + x*4
			img.Pix[d] = c.raw[s+2]
			img.Pix[d+1] = c.raw[s+1]
			img.Pix[d+2] = c.raw[s]
			img.Pix[d+3] = 255
		}
	}
	return img, nil
}
func (c *Camera) Close() {
	if !c.closed {
		C.ac_camera_stop()
		c.closed = true
	}
}

type Audio struct{ closed bool }

func OpenAudio() (*Audio, error) {
	var message [1024]C.char
	if C.ac_audio_start(&message[0], 1024) == 0 {
		return nil, errors.New(C.GoString(&message[0]))
	}
	return &Audio{}, nil
}
func (a *Audio) Read() []int16 {
	samples := make([]int16, 960)
	if C.ac_audio_read(unsafe.Pointer(&samples[0]), 1920) == 0 {
		return nil
	}
	return samples
}
func (a *Audio) Write(samples []int16) {
	if len(samples) == 960 && !a.closed {
		C.ac_audio_write(unsafe.Pointer(&samples[0]), 1920)
	}
}
func (a *Audio) Close() {
	if !a.closed {
		C.ac_audio_stop()
		a.closed = true
	}
}
