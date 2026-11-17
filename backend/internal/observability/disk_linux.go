//go:build linux

package observability

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

func diskFreeRatio() (float64, bool) {
	var s syscall.Statfs_t
	if syscall.Statfs("/", &s) != nil || s.Blocks == 0 {
		return 0, false
	}
	return float64(s.Bavail) / float64(s.Blocks), true
}

func processMetrics() map[string]float64 {
	out := map[string]float64{}
	var u syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &u) == nil {
		out["process_cpu_seconds_total"] = float64(u.Utime.Sec+u.Stime.Sec) + float64(u.Utime.Usec+u.Stime.Usec)/1e6
	}
	if b, e := os.ReadFile("/proc/self/statm"); e == nil {
		f := strings.Fields(string(b))
		if len(f) > 1 {
			if n, e := strconv.ParseUint(f[1], 10, 64); e == nil {
				out["process_resident_bytes"] = float64(n) * float64(os.Getpagesize())
			}
		}
	}
	if entries, e := os.ReadDir("/proc/self/fd"); e == nil {
		out["process_open_fds"] = float64(len(entries))
		var sockets float64
		for _, f := range entries {
			if target, e := os.Readlink("/proc/self/fd/" + f.Name()); e == nil && strings.HasPrefix(target, "socket:[") {
				sockets++
			}
		}
		out["process_socket_fds"] = sockets
	}
	return out
}
