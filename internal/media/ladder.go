package media

import (
	"fmt"
	"math"
)

// Rung is one quality of the HLS stream.
type Rung struct {
	Name string
	// Width and Height of the output. Both are even, as H.264 needs.
	Width  int
	Height int
	// VideoKbps and AudioKbps are the target bitrates in kilobits per second.
	VideoKbps int
	AudioKbps int
}

// The qualities that deneb makes, by the short side of the picture. A source never
// gets a quality larger than itself.
var ladder = []struct {
	short     int
	videoKbps int
	audioKbps int
}{
	{1080, 5000, 192},
	{720, 2800, 128},
	{480, 1400, 128},
	{360, 800, 96},
}

// Ladder returns the qualities for a source of the given displayed size, the
// largest first. A source smaller than 360p gets one quality of its own size.
func Ladder(width, height int) []Rung {
	short := min(width, height)
	rungs := []Rung{}
	for _, step := range ladder {
		if step.short > short {
			continue
		}
		w, h := fit(width, height, step.short)
		rungs = append(rungs, Rung{Name: fmt.Sprintf("%dp", step.short), Width: w, Height: h, VideoKbps: step.videoKbps, AudioKbps: step.audioKbps})
	}
	if len(rungs) == 0 {
		smallest := ladder[len(ladder)-1]
		w, h := even(width), even(height)
		rungs = append(rungs, Rung{Name: fmt.Sprintf("%dp", min(w, h)), Width: w, Height: h, VideoKbps: smallest.videoKbps, AudioKbps: smallest.audioKbps})
	}
	return rungs
}

// fit scales a size so that its short side is the given length, keeping the shape.
func fit(width, height, short int) (int, int) {
	if width >= height {
		return even(int(math.Round(float64(width) * float64(short) / float64(height)))), short
	}
	return short, even(int(math.Round(float64(height) * float64(short) / float64(width))))
}

func even(value int) int {
	if value < 2 {
		return 2
	}
	return value - value%2
}
