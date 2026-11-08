package ai

import (
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createNoWindow is the CREATE_NO_WINDOW process creation flag.
const createNoWindow = 0x08000000

// configureProcess hides the console window of the child process.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
	jobErr    error
)

// appJob returns the job object of the app. It is created with KILL_ON_JOB_CLOSE: when the
// app exits, Windows closes its last handle and ends every process still in the job.
func appJob() (windows.Handle, error) {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			jobErr = err
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			_ = windows.CloseHandle(h)
			jobErr = err
			return
		}
		jobHandle = h
	})
	return jobHandle, jobErr
}

// joinJob puts a started child process into the app's job object. The short window between
// Start and this call is covered by the taskkill fallback in killProcessTree.
func joinJob(p *os.Process) error {
	if p == nil {
		return nil
	}
	job, err := appJob()
	if err != nil {
		return err
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(ph) }()
	return windows.AssignProcessToJobObject(job, ph)
}

// killProcessTree ends p and all its children. taskkill /T walks the tree, which
// Process.Kill alone does not; Process.Kill is the fallback.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	tk := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid))
	configureProcess(tk)
	if err := tk.Run(); err == nil {
		return nil
	}
	return p.Kill()
}
