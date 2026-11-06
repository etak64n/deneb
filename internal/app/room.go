package app

import (
	"fmt"

	"github.com/etak64n/deneb/internal/media"
)

// diskReserve is the free disk that deneb keeps for its database. An upload that would
// leave less is refused, and an encoding waits, since SQLite cannot write on a full disk.
const diskReserve = 1 << 30

// roomError says that the disk of deneb has too little free space for an encoding,
// which waits for it without counting as an attempt.
type roomError struct{ need, free uint64 }

func (e roomError) Error() string {
	return fmt.Sprintf("waiting for free disk: the encoding needs %s, and the disk of deneb has %s", gigabytes(e.need), gigabytes(e.free))
}

// gigabytes writes a size for people, such as "12.3 GB".
func gigabytes(n uint64) string { return fmt.Sprintf("%.1f GB", float64(n)/1e9) }

// RoomForUpload returns an error that says why the disk cannot take a file of size
// bytes and still keep diskReserve free, or nil when it can or deneb cannot tell.
func (a *App) RoomForUpload(size int64) error {
	free, ok := freeSpace(a.cfg.DataDir)
	file := uint64(max(size, 0))
	if !ok || free >= file+diskReserve {
		return nil
	}
	return fmt.Errorf("the disk of deneb has %s free, too little for a file of %s and the %s that deneb keeps for its database",
		gigabytes(free), gigabytes(file), gigabytes(diskReserve))
}

// roomForEncoding checks that the disk can take the stream of a video: its rungs at
// their bitrates for its duration, a tenth more for the playlists and pictures, and
// diskReserve. The stream moves from the work folder into the storage, which takes
// no more room on the volume.
func (a *App) roomForEncoding(probe media.Probe, rungs []media.Rung) error {
	free, ok := freeSpace(a.cfg.DataDir)
	if !ok {
		return nil
	}
	bitrate := 0
	for _, rung := range rungs {
		bitrate += (rung.VideoKbps + rung.AudioKbps) * 1000
	}
	need := uint64(float64(bitrate)/8*probe.Duration*1.1) + diskReserve
	if free >= need {
		return nil
	}
	return roomError{need: need, free: free}
}
