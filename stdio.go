package msgpgen

import (
	"bytes"
	"io"
	"os"
	"sync"
)

var captureLock sync.Mutex

// CaptureStdio allows you to capture standard input and standard
// output for a block of code.
//
// It is intended to allow you to capture the output of bad citizen
// libraries which write to stdout and/or stderr instead of a
// user-supplied writer.
//
// Warning: this is single threaded. It is a feature of absolute last resort.
// I'd almost suggest forking whatever library you are dealing with before
// using this. Don't use it. Please don't use it. You really don't need to use
// it.
//
func captureStdio(do func() error) (stdout, stderr bytes.Buffer, err error) {
	captureLock.Lock()
	defer captureLock.Unlock()

	// https://stackoverflow.com/questions/10473800/in-go-how-do-i-capture-stdout-of-a-function-into-a-string
	oldStdout, oldStderr := os.Stdout, os.Stderr
	or, ow, _ := os.Pipe()
	er, ew, _ := os.Pipe()
	os.Stdout, os.Stderr = ow, ew

	outC, errC := make(chan bytes.Buffer), make(chan bytes.Buffer)
	// copy the output in a separate goroutine so printing can't block indefinitely
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, or)
		outC <- buf
	}()
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, er)
		errC <- buf
	}()

	err = do()

	// back to normal state
	ow.Close()
	ew.Close()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	stdout = <-outC
	stderr = <-errC
	return
}
