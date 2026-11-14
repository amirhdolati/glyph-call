//go:build !darwin || !cgo

package devices

import (
	"errors"
	"image"
)

func Supported() bool   { return false }
func Cameras() []string { return nil }

type Camera struct{}

func OpenCamera(index int) (*Camera, error) {
	return nil, errors.New("native camera capture currently requires macOS 14+ and a CGO build")
}
func (c *Camera) Read() (image.Image, error) { return nil, errors.New("camera unavailable") }
func (c *Camera) Close()                     {}

type Audio struct{}

func OpenAudio() (*Audio, error) {
	return nil, errors.New("native audio capture currently requires macOS 14+ and a CGO build")
}
func (a *Audio) Read() []int16         { return nil }
func (a *Audio) Write(samples []int16) {}
func (a *Audio) Close()                {}
