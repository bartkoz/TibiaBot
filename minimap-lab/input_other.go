//go:build !windows && !darwin

package main

func newSystemEmitter() (Emitter, error) { return nil, ErrUnsupported }
