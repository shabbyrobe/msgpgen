package msgpgen

import (
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

func findImportedName(name, originPkg string) string {
	name = filepath.Base(name)
	originPkg = filepath.Base(originPkg) + "."
	return strings.TrimPrefix(name, originPkg)
}

func splitType(t string) (pkg, name string, err error) {
	lidx := strings.LastIndex(t, ".")
	if lidx >= 0 {
		pkg, name = t[0:lidx], t[lidx+1:]
	} else {
		err = errors.Errorf("could not parse '%s', expected format full/pkg/path.Type", t)
	}
	return
}
