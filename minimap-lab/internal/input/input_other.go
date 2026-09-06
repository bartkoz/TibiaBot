//go:build !windows && !darwin

package input

func NewSystemEmitter() (Emitter, error) { return nil, ErrUnsupported }
