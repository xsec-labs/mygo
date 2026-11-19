//go:build !darwin

package main

import "errors"

func measure(mode string, events, warmup, x, y, boxes, webviews int) error {
	return errors.New("measures macOS only")
}
