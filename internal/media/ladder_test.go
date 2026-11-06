package media

import (
	"fmt"
	"reflect"
	"testing"
)

func TestLadder(t *testing.T) {
	type size struct{ name, dims string }
	sizes := func(rungs []Rung) []size {
		out := make([]size, 0, len(rungs))
		for _, rung := range rungs {
			out = append(out, size{rung.Name, fmt.Sprintf("%dx%d", rung.Width, rung.Height)})
		}
		return out
	}
	cases := []struct {
		width, height int
		want          []size
	}{
		{1920, 1080, []size{{"1080p", "1920x1080"}, {"720p", "1280x720"}, {"480p", "852x480"}, {"360p", "640x360"}}},
		// A video recorded upright on a phone keeps its shape.
		{1080, 1920, []size{{"1080p", "1080x1920"}, {"720p", "720x1280"}, {"480p", "480x852"}, {"360p", "360x640"}}},
		// No quality is larger than the source.
		{1280, 720, []size{{"720p", "1280x720"}, {"480p", "852x480"}, {"360p", "640x360"}}},
		{3840, 2160, []size{{"1080p", "1920x1080"}, {"720p", "1280x720"}, {"480p", "852x480"}, {"360p", "640x360"}}},
		// Sizes stay even, as H.264 needs.
		{641, 361, []size{{"360p", "638x360"}}},
		// A source smaller than 360p gets one quality of its own size.
		{320, 240, []size{{"240p", "320x240"}}},
		{321, 241, []size{{"240p", "320x240"}}},
	}
	for _, c := range cases {
		if got := sizes(Ladder(c.width, c.height)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Ladder(%d, %d) = %v, want %v", c.width, c.height, got, c.want)
		}
	}
}

func TestTimestamp(t *testing.T) {
	cases := map[float64]string{0: "00:00:00.000", 2: "00:00:02.000", 61.25: "00:01:01.250", 3723.5: "01:02:03.500"}
	for seconds, want := range cases {
		if got := timestamp(seconds); got != want {
			t.Errorf("timestamp(%g) = %s, want %s", seconds, got, want)
		}
	}
}

func TestProgressReader(t *testing.T) {
	var reports []float64
	read := progressReader(10, func(fraction float64) { reports = append(reports, fraction) })
	for _, line := range []string{"frame=10", "out_time_us=2500000", "out_time_us=N/A", "out_time_us=-1", "out_time_us=20000000"} {
		read(line)
	}
	if want := []float64{0.25, 1}; !reflect.DeepEqual(reports, want) {
		t.Errorf("reports %v, want %v", reports, want)
	}
}

func TestQuarterTurn(t *testing.T) {
	type side = []struct {
		Rotation float64 `json:"rotation"`
	}
	cases := []struct {
		tag  string
		side side
		want bool
	}{
		{"", nil, false},
		{"90", nil, true},
		{"180", nil, false},
		{"270", nil, true},
		{"", side{{Rotation: -90}}, true},
		{"", side{{Rotation: 0}}, false},
	}
	for _, c := range cases {
		if got := quarterTurn(c.tag, c.side); got != c.want {
			t.Errorf("quarterTurn(%q, %v) = %v", c.tag, c.side, got)
		}
	}
}
