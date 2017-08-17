package msgpgen

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
)

type Cleanup struct {
	stack []string
}

func (c *Cleanup) Push(path string) { c.stack = append(c.stack, path) }

func (c *Cleanup) Cleanup(err *error) {
	for i := len(c.stack) - 1; i >= 0; i-- {
		cur := c.stack[i]
		if cerr := os.RemoveAll(cur); cerr != nil && *err == nil {
			*err = cerr
		}
	}
}

type TempFile struct {
	Name string
	Keep bool
	file *os.File
}

func OpenTempFile(dir, prefix string) (t *TempFile, err error) {
	t = &TempFile{}
	t.file, err = ioutil.TempFile(dir, prefix)
	if err != nil {
		return
	}
	t.Name = t.file.Name()
	return t, nil
}

func TouchTempFile(dir, prefix string) (t *TempFile, err error) {
	t, err = OpenTempFile(dir, prefix)
	if err != nil {
		return
	}
	if err = t.Close(); err != nil {
		t.Cleanup(nil)
		return
	}
	return
}

func (t *TempFile) Reopen(offset int64, whence int) (n int64, err error) {
	if t.file != nil {
		err = fmt.Errorf("file already open")
		return
	}
	t.file, err = os.OpenFile(t.Name, os.O_RDWR|os.O_EXCL, 0600)
	if err != nil {
		return
	}
	return t.file.Seek(offset, whence)
}

func (t *TempFile) Copy(dest string, perms os.FileMode) (err error) {
	var f *os.File
	f, err = os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perms)
	if err != nil {
		return err
	}
	defer func() {
		cerr := f.Close()
		if err == nil {
			err = cerr
		}
	}()
	if t.file == nil {
		if _, err = t.Reopen(0, 0); err != nil {
			return err
		}
	}
	if _, err = io.Copy(f, t); err != nil {
		return err
	}
	return
}

func (t *TempFile) Read(p []byte) (n int, err error) {
	return t.file.Read(p)
}

func (t *TempFile) Write(p []byte) (n int, err error) {
	return t.file.Write(p)
}

func (t *TempFile) Seek(offset int64, whence int) (int64, error) {
	return t.file.Seek(offset, whence)
}

func (t *TempFile) Close() error {
	var e error
	e = t.file.Close()
	t.file = nil
	return e
}

func (t *TempFile) Cleanup(err *error) {
	if t == nil {
		return
	}
	var cerr error
	if t.file != nil {
		if cerr = t.file.Close(); *err == nil {
			*err = cerr
		}
		t.file = nil
	}

	if !t.Keep && t.Name != "" {
		if cerr = os.Remove(t.Name); *err == nil {
			*err = cerr
		}
		t.Name = ""
	}
}
