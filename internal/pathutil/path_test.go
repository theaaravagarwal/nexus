package pathutil

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestExpandUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain path", in: "/tmp/demo", want: "/tmp/demo"},
		{name: "just tilde", in: "~", want: home},
		{name: "tilde slash", in: "~/projects", want: filepath.Join(home, "projects")},
		{name: "tilde backslash", in: "~\\projects", want: filepath.Join(home, "projects")},
		{name: "other user form untouched", in: "~alice/projects", want: "~alice/projects"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpandUser(tc.in)
			if err != nil {
				t.Fatalf("ExpandUser(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ExpandUser(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeForRsync(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		in := "C:\\work\\data"
		if got := NormalizeForRsync(in); got != in {
			t.Fatalf("NormalizeForRsync(%q)=%q want %q on non-windows", in, got, in)
		}
		return
	}

	tests := []struct {
		in   string
		want string
	}{
		{in: `C:\\work\\data`, want: `/c/work/data`},
		{in: `D:data`, want: `/d/data`},
		{in: `/already/unix`, want: `/already/unix`},
	}
	for _, tc := range tests {
		if got := NormalizeForRsync(tc.in); got != tc.want {
			t.Fatalf("NormalizeForRsync(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
