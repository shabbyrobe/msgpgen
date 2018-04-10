package msgpgen

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/shabbyrobe/structer"
)

type Directives struct {
	tpset *structer.TypePackageSet

	directives []Directive

	// Maps fully qualified type names to the locally referenced name
	// in the directive
	ignore map[structer.TypeName]string

	intercepted map[structer.TypeName]string

	tuple      map[structer.TypeName]string
	allowextra map[structer.TypeName]string
	shim       map[structer.TypeName]*ShimDirective
	pkg        string
}

func NewDirectives(tpset *structer.TypePackageSet, pkg string) *Directives {
	d := &Directives{
		tpset:       tpset,
		ignore:      make(map[structer.TypeName]string),
		intercepted: make(map[structer.TypeName]string),
		tuple:       make(map[structer.TypeName]string),
		allowextra:  make(map[structer.TypeName]string),
		shim:        make(map[structer.TypeName]*ShimDirective),
		pkg:         pkg,
	}
	return d
}

func (d *Directives) load() error {
	directives, err := loadDirectives(d.tpset, d.pkg)
	if err != nil {
		return err
	}
	for _, dir := range directives {
		if err := d.add(dir); err != nil {
			return err
		}
	}
	return nil
}

func (d *Directives) add(dirs ...Directive) error {
	for _, dir := range dirs {
		d.directives = append(d.directives, dir)

		switch dir := dir.(type) {
		case *ShimDirective:
			tn, err := structer.ParseLocalName(dir.Type, d.pkg)
			if err != nil {
				return err
			}
			d.shim[tn] = dir

		case *InterceptDirective:
			tn, err := structer.ParseLocalName(dir.Type, d.pkg)
			if err != nil {
				return err
			}
			d.intercepted[tn] = dir.Type

		case *IgnoreDirective:
			for _, t := range dir.Types {
				tn, err := structer.ParseLocalName(t, d.pkg)
				if err != nil {
					return err
				}
				d.ignore[tn] = t
			}

		case *TupleDirective:
			for _, t := range dir.Types {
				tn, err := structer.ParseLocalName(t, d.pkg)
				if err != nil {
					return err
				}
				d.tuple[tn] = t
			}

		case *AllowExtraDirective:
			for _, t := range dir.Types {
				tn, err := structer.ParseLocalName(t, d.pkg)
				if err != nil {
					return err
				}
				d.allowextra[tn] = t
			}

		default:
			return errors.Errorf("Unknown msgp directive %+v", dir)
		}
	}
	return nil
}

// find all comment lines that begin with //msgp:
func loadDirectives(tpset *structer.TypePackageSet, pkg string) ([]Directive, error) {
	var d []Directive

	if _, ok := tpset.BuiltFiles[pkg]; !ok {
		return nil, errors.Errorf("could not find built files for package %s", pkg)
	}

	for _, fname := range tpset.BuiltFiles[pkg] {
		astPkg := tpset.ASTPackages.Packages[pkg]
		if astPkg == nil {
			return nil, (errors.Errorf("could not find ast package %s", pkg))
		}
		fileAST := astPkg.FileASTs[fname]
		if fileAST == nil {
			return nil, (errors.Errorf("could not find file %s in package %s", fname, pkg))
		}

		for _, cg := range fileAST.Comments {
			for _, line := range cg.List {
				if strings.HasPrefix(line.Text, linePrefix) {
					dir, err := ParseDirective(strings.TrimPrefix(line.Text, linePrefix))
					if err != nil {
						return nil, err
					}
					d = append(d, dir)
				}
			}
		}
	}
	return d, nil
}

type DirectivesCache struct {
	pkgDirectives map[string]*Directives
	tpset         *structer.TypePackageSet
}

func NewDirectivesCache(tpset *structer.TypePackageSet) *DirectivesCache {
	return &DirectivesCache{
		tpset:         tpset,
		pkgDirectives: make(map[string]*Directives),
	}
}

func (d *DirectivesCache) Ignored(dctvs *Directives, fullName structer.TypeName) bool {
	if _, ok := dctvs.ignore[fullName]; ok {
		return true
	}
	return false
}

func (d *DirectivesCache) IgnoredPkg(fullName structer.TypeName) (bool, error) {
	dctvs, err := d.Ensure(fullName.PackagePath)
	if err != nil {
		return false, err
	}
	if dctvs == nil {
		return false, nil
	}
	if _, ok := dctvs.ignore[fullName]; ok {
		return true, nil
	}
	return false, nil
}

func (d *DirectivesCache) Ensure(pkg string) (*Directives, error) {
	var drctvs *Directives
	var ok bool
	var err error
	if drctvs, ok = d.pkgDirectives[pkg]; !ok {
		drctvs = NewDirectives(d.tpset, pkg)
		if err = drctvs.load(); err != nil {
			return nil, err
		}
		d.pkgDirectives[pkg] = drctvs
	}
	return drctvs, err
}
