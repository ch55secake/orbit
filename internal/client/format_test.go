package client

import "testing"

func TestFormatBytesSupportsLargeCapacities(t *testing.T) {
	for _, test := range []struct {
		value uint64
		want  string
	}{
		{0, "0 B"},
		{1024, "1.0 KiB"},
		{1 << 40, "1.0 TiB"},
		{1 << 50, "1.0 PiB"},
		{1 << 60, "1.0 EiB"},
		{^uint64(0), "16.0 EiB"},
	} {
		if got := FormatBytes(test.value); got != test.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", test.value, got, test.want)
		}
	}
}
