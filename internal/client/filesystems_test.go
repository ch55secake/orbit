package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestClientDecodesOptionalFilesystems(t *testing.T) {
	for _, fixture := range []string{"metrics-complete.json", "metrics-filesystems.json", "metrics-filesystems-partial.json"} {
		t.Run(fixture, func(t *testing.T) {
			server := fixtureServer(t, "/api/v1/metrics", http.StatusOK, readFixture(t, fixture))
			defer server.Close()
			response, err := New(time.Second).Metrics(context.Background(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			data := response.Data
			if data.Disk != (Disk{Path: "/", UsedBytes: 10737418240, TotalBytes: 42949672960, UsedPercent: 25}) || data.Memory.UsedPercent != 37.5 {
				t.Fatal("filesystem extension changed legacy disk/memory")
			}
			if fixture == "metrics-complete.json" {
				if len(data.Filesystems) != 0 {
					t.Fatal("legacy response acquired filesystems")
				}
				return
			}
			if len(data.Filesystems) != 2 || data.Filesystems[0] != (Filesystem{MountPoint: "/", Filesystem: "ext4", Device: "/dev/sda1", UsedBytes: 10737418240, TotalBytes: 42949672960, AvailableBytes: 30064771072, UsedPercent: 25}) {
				t.Fatalf("filesystems = %+v", data.Filesystems)
			}
			if fixture == "metrics-filesystems.json" {
				if data.Filesystems[1].MountPoint != "/data" || data.Filesystems[1].UsedPercent != 95 || data.Filesystems[1].AvailableBytes != 3221225472 {
					t.Fatal("additional mount fields lost")
				}
			} else if len(data.Errors) != 1 || data.Errors[0].Metric != "filesystems" || data.Filesystems[1].TotalBytes != 0 {
				t.Fatal("partial samples/errors lost")
			}
		})
	}
}

func TestAbsentNullAndEmptyFilesystems(t *testing.T) {
	for _, field := range []string{"", `,"filesystems":null`, `,"filesystems":[]`} {
		var response MetricsResponse
		if err := json.Unmarshal([]byte(`{"api_version":"v1","data":{"disk":{"path":"/"}`+field+`}}`), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Data.Filesystems) != 0 || response.Data.Disk.Path != "/" {
			t.Fatal("empty filesystem list changed legacy disk")
		}
	}
}
