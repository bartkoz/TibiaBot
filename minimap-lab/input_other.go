//go:build !windows

package main

func newSystemEmitter() (Emitter, error) { return nil, ErrUnsupported }
