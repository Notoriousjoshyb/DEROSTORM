//go:build linux && amd64

package main

import (
	"reflect"
	"testing"
)

// The bug this file exists for: 1.7.2 spelled the AMD candidates out as
// libderostorm_hip6.so and libderostorm_hip5.so, so a rig with ROCm 7 -- which
// ships only libamdhip64.so.7 -- resolved neither, and the HIP backend went as
// quiet as it does on a machine with no AMD card. The list is scanned now, so
// what matters is that a new generation sorts ahead of the old ones and that
// nothing else in the directory is mistaken for a library.
func TestHipLibOrder(t *testing.T) {
	for _, tc := range []struct {
		name  string
		names []string
		want  []string
	}{
		{
			name: "newest ROCm first, whatever order the directory is in",
			names: []string{
				"libderostorm_hip5.so", "README.md",
				"libderostorm_hip7.so", "libderostorm_hip6.so",
			},
			want: []string{
				"gpulib/linux/libderostorm_hip7.so",
				"gpulib/linux/libderostorm_hip6.so",
				"gpulib/linux/libderostorm_hip5.so",
			},
		},
		{
			// Sorted by number and not by string: 10 is newer than 9, and
			// "10" sorts before "9".
			name:  "two digits beat one",
			names: []string{"libderostorm_hip9.so", "libderostorm_hip10.so"},
			want: []string{
				"gpulib/linux/libderostorm_hip10.so",
				"gpulib/linux/libderostorm_hip9.so",
			},
		},
		{
			name: "anything not a versioned library is ignored",
			names: []string{
				"README.md", "libderostorm_hip.so", "libderostorm_hip6.so.bak",
				"7.so", "libderostorm_gpu.so", "libderostorm_hipX.so",
			},
			want: []string{},
		},
		{
			// A tree where nobody has run gpu/buildlib_hip.sh. Not an error:
			// the backend reports no devices, as on a machine with no AMD card.
			name:  "no libraries at all",
			names: []string{"README.md"},
			want:  []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := hipLibOrder(tc.names)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("hipLibOrder(%v)\n got %v\nwant %v", tc.names, got, tc.want)
			}
		})
	}
}
