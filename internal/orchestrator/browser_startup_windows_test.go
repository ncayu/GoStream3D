//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWaitCDPEndpointAllowsSuccessfulLauncherExit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) < 3 {
			http.Error(w, "browser is still starting", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`[{"type":"page","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/page/ready"}]`))
	}))
	defer server.Close()
	process := make(chan error, 1)
	process <- nil // Edge's launcher can hand off to a replacement process.
	close(process)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	endpoint, err := waitCDPEndpoint(ctx, server.Listener.Addr().(*net.TCPAddr).Port, process)
	if err != nil || endpoint != "ws://127.0.0.1/devtools/page/ready" {
		t.Fatalf("replacement browser was not awaited: endpoint=%q error=%v", endpoint, err)
	}
}

// Re-execute the test binary as a launcher that exits immediately after spawning
// a real Edge/Chrome process, reproducing the lifecycle rather than mocking it.
func TestWindowsBrowserProcessHelper(t *testing.T) {
	if os.Getenv("G3D_TEST_BROWSER_HANDOFF") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg != "--" || len(os.Args) <= i+3 {
			continue
		}
		browser, port, profile := os.Args[i+1], os.Args[i+2], os.Args[i+3]
		args := []string{"--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-extensions",
			"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=" + port,
			"--user-data-dir=" + profile, "--edge-skip-compat-layer-relaunch", "about:blank"}
		child := exec.Command(browser, args...)
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(filepath.Join(profile, "handoff.pid"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			_ = child.Process.Kill()
			os.Exit(3)
		}
		os.Exit(0)
	}
	os.Exit(4)
}

func TestWindowsBrowserJobCleansReplacementAfterLauncherExit(t *testing.T) {
	if os.Getenv("G3D_TEST_NESTED_BROWSER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsBrowserJobCleansReplacementAfterLauncherExit$", "-test.v")
		cmd.Env = append(os.Environ(), "G3D_TEST_NESTED_BROWSER=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("browser handoff helper failed: %v\n%s", err, output)
		}
		return
	}
	browser, err := findWindowsBrowser("chromium")
	if err != nil {
		t.Skip(err)
	}
	// Match the production hierarchy: browser job nested in a gateway CPU job.
	parentJob, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(parentJob)
	cpu := struct{ Flags, Rate uint32 }{0x1 | 0x4, 5000}
	if _, err := windows.SetInformationJobObject(parentJob, windows.JobObjectCpuRateControlInformation,
		uintptr(unsafe.Pointer(&cpu)), uint32(unsafe.Sizeof(cpu))); err != nil {
		t.Fatal(err)
	}
	current, err := windows.GetCurrentProcess()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.AssignProcessToJobObject(parentJob, current); err != nil {
		t.Fatal(err)
	}
	port := freeTestPort(t, 37300)
	profile := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsBrowserProcessHelper$", "--", browser, strconv.Itoa(port), profile)
	cmd.Env = append(os.Environ(), "G3D_TEST_BROWSER_HANDOFF=1")
	job, err := startWindowsBrowser(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if job != 0 {
			_ = windows.TerminateJobObject(job, 1)
			_ = windows.CloseHandle(job)
		}
	})
	process := make(chan error, 1)
	go func() { process <- cmd.Wait(); close(process) }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	select {
	case err := <-process:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// At this point the managed PID is gone but the real browser is starting.
	if _, err := waitCDPEndpoint(ctx, port, process); err != nil {
		t.Fatal(err)
	}
	pidData, err := os.ReadFile(filepath.Join(profile, "handoff.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		t.Fatal(err)
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	if err := stopWindowsBrowserJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	job = 0
	status, err := windows.WaitForSingleObject(child, 0)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("replacement browser survived shutdown: status=%d error=%v", status, err)
	}
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond); err == nil {
		conn.Close()
		t.Fatal("DevTools port was still owned after cleanup")
	}
}

func TestWaitCDPEndpointFailureAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	t.Run("crash", func(t *testing.T) {
		crash := errors.New("exit status 17")
		process := make(chan error, 1)
		process <- crash
		close(process)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := waitCDPEndpoint(ctx, port, process)
		if !errors.Is(err, crash) {
			t.Fatalf("lost browser exit error: %v", err)
		}
	})
	t.Run("clean exit without DevTools", func(t *testing.T) {
		process := make(chan error, 1)
		process <- nil
		close(process)
		ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
		defer cancel()
		_, err := waitCDPEndpoint(ctx, port, process)
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "exited successfully") {
			t.Fatalf("expected bounded wait with a meaningful diagnostic: %v", err)
		}
	})
	t.Run("cancel during handoff", func(t *testing.T) {
		process := make(chan error, 1)
		process <- nil
		close(process)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := waitCDPEndpoint(ctx, port, process)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation was not preserved: %v", err)
		}
	})
}
