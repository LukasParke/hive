//go:build !linux

package monitor

type syscallStatfs struct {
	Bsize  int64
	Blocks uint64
	Bfree  uint64
}

func statfs(_ string, _ *syscallStatfs) error {
	return nil
}
