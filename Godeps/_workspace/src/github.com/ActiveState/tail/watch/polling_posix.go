// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.
// +build linux darwin

package watch

func permissionErrorRetry(err error, retry *int) bool {
	// No need for this on linux, don't retry
	return false
}
