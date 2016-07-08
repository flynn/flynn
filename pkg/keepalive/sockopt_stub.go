// +build !linux

package keepalive

func setSockopt(fd int) error {
	return nil
}
