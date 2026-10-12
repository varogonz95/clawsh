//go:build !linux && !darwin && !windows

package service

import "errors"

var errUnsupported = errors.New("install-service: unsupported on this OS")

func Install(Options, Env) error { return errUnsupported }

func Uninstall(Env) error { return errUnsupported }
