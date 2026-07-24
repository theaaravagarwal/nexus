package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseMetadataCollectsStructuredSnapshot(t *testing.T) {
	input := strings.Join([]string{
		"OS=Ubuntu 24.04",
		"CPU=Example CPU",
		"MEMORY_BYTES=34359738368",
		"GPU=NVIDIA RTX 4090 · 25.8 GB VRAM",
		"GPU=AMD Radeon",
		"DISK=/dev/nvme0n1p2\t/\t500000000000\t400000000000\t1000000000000",
		"DISK=/dev/sda1\t/data\t1000000000000\t1000000000000\t2000000000000",
		"TOOLS=btop,duf,df",
		"SECRET=nope",
	}, "\n")

	got := parseMetadata(input)
	if got.OS != "Ubuntu 24.04" || got.CPU != "Example CPU" || got.MemoryBytes != 34359738368 {
		t.Fatalf("identity/memory fields=%#v", got)
	}
	if len(got.GPUs) != 2 || got.GPUs[0] != "NVIDIA RTX 4090 · 25.8 GB VRAM" {
		t.Fatalf("GPUs=%v", got.GPUs)
	}
	if len(got.Disks) != 2 {
		t.Fatalf("disks=%#v", got.Disks)
	}
	root := got.Disks[0]
	if root.Filesystem != "/dev/nvme0n1p2" || root.Mountpoint != "/" ||
		root.UsedBytes != 500000000000 || root.AvailableBytes != 400000000000 ||
		root.TotalBytes != 1000000000000 {
		t.Fatalf("root disk=%#v", root)
	}
	if strings.Join(got.Tools, ",") != "btop,duf,df" {
		t.Fatalf("tools=%v", got.Tools)
	}
}

func TestParseMetadataSanitizesANSIControlsAndUnknownFields(t *testing.T) {
	got := parseMetadata(
		"OS=\x1b[31mUbuntu\x1b[0m\n" +
			"CPU=Example\x1b]52;c;secret\a CPU\n" +
			"GPU=\x1b[32mGPU\x1b[0m\n" +
			"TOOLS=btop,\x1b[31mduf\x1b[0m,\u009b32mncdu\u009b0m\n" +
			"SECRET=nope\n",
	)
	if got.OS != "Ubuntu" || got.CPU != "Example CPU" {
		t.Fatalf("sanitized snapshot=%#v", got)
	}
	if len(got.GPUs) != 1 || got.GPUs[0] != "GPU" {
		t.Fatalf("GPUs=%v", got.GPUs)
	}
	if strings.Join(got.Tools, ",") != "btop,duf,ncdu" {
		t.Fatalf("tools=%v", got.Tools)
	}
}

func TestParseMetadataRejectsMalformedNumericAndDiskRecords(t *testing.T) {
	got := parseMetadata(strings.Join([]string{
		"MEMORY_BYTES=not-a-number",
		"DISK=/dev/a\t/\t1\t2",
		"DISK=/dev/b\t/data\tused\t2\t3",
		"DISK=/dev/c\t/data\t1\t2\t0",
	}, "\n"))
	if got.MemoryBytes != 0 || len(got.Disks) != 0 || got.usable() {
		t.Fatalf("malformed metadata accepted: %#v", got)
	}
}

func TestParseMetadataDeduplicatesAndCapsRepeatedRecords(t *testing.T) {
	var input strings.Builder
	input.WriteString("GPU=Same GPU\nGPU=same gpu\n")
	input.WriteString("DISK=/dev/a\t/\t1\t2\t3\nDISK=/dev/a\t/\t1\t2\t3\n")
	input.WriteString("TOOLS=df,DF,duf\n")
	for index := 0; index < maxMetadataGPUs+10; index++ {
		fmt.Fprintf(&input, "GPU=GPU %d\n", index)
	}
	for index := 0; index < maxMetadataDisks+10; index++ {
		fmt.Fprintf(&input, "DISK=/dev/%d\t/mnt/%d\t1\t2\t3\n", index, index)
	}

	got := parseMetadata(input.String())
	if len(got.GPUs) != maxMetadataGPUs {
		t.Fatalf("GPU count=%d, want cap %d", len(got.GPUs), maxMetadataGPUs)
	}
	if len(got.Disks) != maxMetadataDisks {
		t.Fatalf("disk count=%d, want cap %d", len(got.Disks), maxMetadataDisks)
	}
	if strings.Join(got.Tools, ",") != "df,duf" {
		t.Fatalf("tools=%v", got.Tools)
	}
}

func TestGPURecordsMergeBackendsWithoutLosingIdenticalCards(t *testing.T) {
	got := parseMetadata(strings.Join([]string{
		"GPU_RECORD=nvidia\t0000:01:00.0\tNVIDIA GeForce RTX 3060 · 12.9 GB VRAM",
		"GPU_RECORD=nvidia\t0000:02:00.0\tNVIDIA GeForce RTX 3060 · 12.9 GB VRAM",
		"GPU_RECORD=wsl-host\tPCI\\VEN_10DE&DEV_2504\tNVIDIA GeForce RTX 3060",
		"GPU_RECORD=wsl-host\tPCI\\VEN_10DE&DEV_2504&SECOND\tNVIDIA GeForce RTX 3060",
		"GPU_RECORD=wsl-host\tPCI\\VEN_8086&DEV_A780\tIntel(R) UHD Graphics 770",
	}, "\n"))
	want := []string{
		"NVIDIA GeForce RTX 3060 · 12.9 GB VRAM",
		"NVIDIA GeForce RTX 3060 · 12.9 GB VRAM",
		"Intel(R) UHD Graphics 770",
	}
	if !slices.Equal(got.GPUs, want) {
		t.Fatalf("GPUs=%v, want %v", got.GPUs, want)
	}
}

func TestGPURecordBackendFailureOutputDoesNotSuppressOtherSources(t *testing.T) {
	got := parseMetadata(strings.Join([]string{
		"nvidia-smi: driver communication failed",
		"GPU_RECORD=lspci\t0000:04:00.0\tAMD Radeon RX 7900 XTX",
		"GPU_RECORD=system_profiler\t1\tApple M4 Max",
	}, "\n"))
	if !slices.Equal(got.GPUs, []string{"AMD Radeon RX 7900 XTX", "Apple M4 Max"}) {
		t.Fatalf("GPUs=%v", got.GPUs)
	}
}

func TestMetadataScriptIncludesAdditiveWSLAndNativeGPUBackends(t *testing.T) {
	for _, want := range []string{
		"/usr/lib/wsl/lib/nvidia-smi",
		"Get-CimInstance Win32_VideoController",
		"system_profiler SPDisplaysDataType",
		"lspci -D",
		"/sys/class/drm/card[0-9]*",
	} {
		if !strings.Contains(metadataScript, want) {
			t.Fatalf("metadata script missing %q", want)
		}
	}
	if strings.Contains(metadataScript, `[ -z "$gpu_records" ] && command -v lspci`) {
		t.Fatal("GPU discovery regressed to a mutually exclusive fallback chain")
	}
}

func TestStorageInventoryScriptUsesTypedPortableFallback(t *testing.T) {
	for _, want := range []string{"df -PkT", "df -Pk", `printf "DISK=%s\t%s\t%.0f\t%.0f\t%.0f\t%s\n"`} {
		if !strings.Contains(storageInventoryScript, want) {
			t.Fatalf("storage script missing %q", want)
		}
	}
}

func TestParseMetadataKeepsMeaningfulVolumesAndFiltersSystemMounts(t *testing.T) {
	const gib = 1_000_000_000
	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name: "native Linux",
			lines: []string{
				fmt.Sprintf("DISK=tmpfs\t/run\t0\t%d\t%d\ttmpfs", gib, gib),
				fmt.Sprintf("DISK=/dev/nvme0n1p2\t/\t%d\t%d\t%d\text4", gib, gib, 2*gib),
				fmt.Sprintf("DISK=/dev/nvme0n1p1\t/boot/efi\t1\t%d\t%d\tvfat", gib, gib),
				fmt.Sprintf("DISK=/dev/loop0\t/snap/core/1\t%d\t0\t%d\tsquashfs", gib, gib),
				fmt.Sprintf("DISK=/dev/sda\t/data1\t%d\t%d\t%d\text4", gib, gib, 2*gib),
				fmt.Sprintf("DISK=/dev/sdb1\t/run/media/user/USB\t%d\t%d\t%d\texfat", gib, 3*gib, 4*gib),
			},
			want: []string{"/dev/nvme0n1p2@/", "/dev/sda@/data1", "/dev/sdb1@/run/media/user/USB"},
		},
		{
			name: "WSL",
			lines: []string{
				fmt.Sprintf("DISK=none\t/usr/lib/wsl/lib\t0\t%d\t%d\toverlay", gib, gib),
				fmt.Sprintf("DISK=drivers\t/usr/lib/wsl/drivers\t%d\t%d\t%d\t9p", gib, gib, 2*gib),
				fmt.Sprintf("DISK=/dev/sdd\t/\t%d\t%d\t%d\text4", gib, gib, 2*gib),
				fmt.Sprintf("DISK=C:\\\t/mnt/c\t%d\t%d\t%d\t9p", gib, gib, 2*gib),
				fmt.Sprintf("DISK=D:\\\t/mnt/d\t%d\t%d\t%d\t9p", gib, 3*gib, 4*gib),
				fmt.Sprintf("DISK=snapfuse\t/snap/snapd/1\t%d\t0\t%d\tfuse.snapfuse", gib, gib),
			},
			want: []string{"/dev/sdd@/", `C:\@/mnt/c`, `D:\@/mnt/d`},
		},
		{
			name: "macOS and remote filesystems",
			lines: []string{
				fmt.Sprintf("DISK=/dev/disk3s1s1\t/\t%d\t%d\t%d\tapfs", gib, gib, 2*gib),
				fmt.Sprintf("DISK=/dev/disk3s5\t/System/Volumes/Data\t%d\t%d\t%d\tapfs", gib, gib, 2*gib),
				fmt.Sprintf("DISK=/dev/disk4s1\t/Volumes/Backup\t%d\t%d\t%d\tapfs", gib, 3*gib, 4*gib),
				fmt.Sprintf("DISK=tank/data\t/data\t%d\t%d\t%d\tzfs", gib, gib, 2*gib),
				fmt.Sprintf("DISK=server:/archive\t/archive\t%d\t%d\t%d\tnfs4", gib, gib, 2*gib),
			},
			want: []string{"/dev/disk3s1s1@/", "server:/archive@/archive", "tank/data@/data", "/dev/disk4s1@/Volumes/Backup"},
		},
		{
			name: "legacy unknown is conservative",
			lines: []string{
				fmt.Sprintf("DISK=custom-volume\t/custom\t%d\t%d\t%d", gib, gib, 2*gib),
				fmt.Sprintf("DISK=tmpfs\t/run/user/1000\t0\t%d\t%d", gib, gib),
			},
			want: []string{"custom-volume@/custom"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := parseMetadata(strings.Join(test.lines, "\n"))
			got := make([]string, 0, len(snapshot.Disks))
			for _, disk := range snapshot.Disks {
				got = append(got, disk.Filesystem+"@"+disk.Mountpoint)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("volumes=%v, want %v", got, test.want)
			}
		})
	}
}

func TestStorageFilteringRetainsDistinctMountsAndRootOverlay(t *testing.T) {
	disks := []diskUsage{
		{Filesystem: "overlay", Mountpoint: "/", FilesystemType: "overlay", TotalBytes: 10},
		{Filesystem: "overlay", Mountpoint: "/containers/a", FilesystemType: "overlay", TotalBytes: 10},
		{Filesystem: "tank", Mountpoint: "/data/a", FilesystemType: "zfs", TotalBytes: 100},
		{Filesystem: "tank", Mountpoint: "/data/b", FilesystemType: "zfs", TotalBytes: 100},
	}
	got := meaningfulStorageDisks(disks)
	if len(got) != 3 || got[0].Mountpoint != "/" {
		t.Fatalf("filtered volumes=%#v", got)
	}
}

func TestFormatDecimalBytesUsesSIUnits(t *testing.T) {
	tests := []struct {
		bytes uint64
		want  string
	}{
		{32 * 1024 * 1024 * 1024, "34.4 GB"},
		{500_000_000_000, "500 GB"},
		{2_000_000_000_000, "2 TB"},
	}
	for _, test := range tests {
		if got := formatDecimalBytes(test.bytes); got != test.want {
			t.Fatalf("formatDecimalBytes(%d)=%q, want %q", test.bytes, got, test.want)
		}
	}
}

func TestNormalizeLegacyCapacityTextUsesDecimalUnits(t *testing.T) {
	tests := map[string]string{
		"124Gi":                "133.1 GB",
		"16 GiB":               "17.2 GB",
		"5 / 20 GiB (25%)":     "5.4 GB / 21.5 GB (25%)",
		"1.6T / 1.8T (91%)":    "1.8 TB / 2 TB (91%)",
		"already 34.4 GB":      "already 34.4 GB",
		"no capacity reported": "no capacity reported",
	}
	for input, want := range tests {
		if got := normalizeLegacyCapacityText(input); got != want {
			t.Fatalf("normalizeLegacyCapacityText(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestLegacyDiskSummaryPrefersRootFilesystem(t *testing.T) {
	disks := []diskUsage{
		{Filesystem: "/dev/data", Mountpoint: "/data", UsedBytes: 1_000_000_000, TotalBytes: 10_000_000_000},
		{Filesystem: "/dev/root", Mountpoint: "/", UsedBytes: 250_000_000_000, TotalBytes: 1_000_000_000_000},
	}
	if got := legacyDiskSummary(disks); got != "250 GB / 1 TB (25%)" {
		t.Fatalf("summary=%q", got)
	}
}

func TestMetadataSnapshotUsableWithAnyStructuredField(t *testing.T) {
	if (hostMetadataSnapshot{}).usable() {
		t.Fatal("empty snapshot is usable")
	}
	for _, snapshot := range []hostMetadataSnapshot{
		{CPU: "CPU"},
		{MemoryBytes: 1},
		{GPUs: []string{"GPU"}},
		{Disks: []diskUsage{{TotalBytes: 1}}},
		{Tools: []string{"df"}},
	} {
		if !snapshot.usable() {
			t.Fatalf("snapshot should be usable: %#v", snapshot)
		}
	}
}

func TestRefreshMetadataPreservesCachedFieldsMissingFromPartialProbe(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(binDir, "ssh"),
		[]byte("#!/bin/sh\nprintf 'CPU=New CPU\\n'\n"),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	statePath := filepath.Join(t.TempDir(), "state.json")
	state := nexusState{Version: 1, Hosts: map[string]hostActivity{
		"alice@example.com": {
			OS: "Cached OS", CPU: "Old CPU", Memory: "34.4 GB",
			GPUs:  []string{"Cached GPU"},
			Disks: []diskUsage{{Filesystem: "/dev/root", Mountpoint: "/", TotalBytes: 1_000_000_000}},
			Tools: []string{"btop"},
		},
	}}
	if err := saveState(statePath, state); err != nil {
		t.Fatal(err)
	}
	if err := refreshHostMetadata(statePath, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	updated, err := loadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := updated.Hosts["alice@example.com"]
	if entry.CPU != "New CPU" || entry.OS != "Cached OS" || entry.Memory != "34.4 GB" ||
		len(entry.GPUs) != 1 || len(entry.Disks) != 1 || len(entry.Tools) != 1 {
		t.Fatalf("partial refresh erased cached metadata: %#v", entry)
	}
}

func TestMetadataOutputIsBoundedWithoutShortWrites(t *testing.T) {
	var output boundedMetadataOutput
	chunk := []byte(strings.Repeat("x", maxMetadataBytes+1024))
	written, err := output.Write(chunk)
	if err != nil || written != len(chunk) {
		t.Fatalf("Write()=(%d, %v), want (%d, nil)", written, err, len(chunk))
	}
	if len(output.String()) != maxMetadataBytes {
		t.Fatalf("captured bytes=%d, want %d", len(output.String()), maxMetadataBytes)
	}
}
