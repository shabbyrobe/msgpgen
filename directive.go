package msgpgen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"github.com/shabbyrobe/structer"
)

const linePrefix = "//msgp:"

type ShimMode string

const (
	Cast    ShimMode = "cast"
	Convert ShimMode = "convert"
)

var ShimModes = map[ShimMode]bool{Cast: true, Convert: true}

type Directive interface {
	Build(tpset *structer.TypePackageSet, pkg string) (string, error)
	Populate(args []string, kwargs map[string]string) error
}

var (
	split    = regexp.MustCompile(`\s+`)
	ident    = `[a-z]+`
	typeName = `[A-Za-z0-9\.\(\)]+`
	keyValue = regexp.MustCompile(`^(` + ident + `):([^\s]*)$`)
	value    = regexp.MustCompile(`^([^\s:]+)$`)
)

func ParseDirective(input string) (Directive, error) {
	parts := split.Split(strings.TrimSpace(input), -1)

	dir := parts[0]

	var args []string
	var kval = make(map[string]string)
	if len(parts) > 1 {
		for _, p := range parts[1:] {
			kvm := keyValue.FindStringSubmatch(p)
			if kvm != nil {
				kval[kvm[1]] = kvm[2]
			} else if value.MatchString(p) {
				args = append(args, p)
			} else {
				return nil, fmt.Errorf("invalid: '%s'", p)
			}
		}
	}

	var directive Directive
	switch dir {
	case "shim":
		directive = &ShimDirective{}
	case "ignore":
		directive = &IgnoreDirective{}
	case "intercept":
		directive = &InterceptDirective{}
	case "tuple":
		directive = &TupleDirective{}
	case "allowextra":
		directive = &AllowExtraDirective{}
	default:
		return nil, fmt.Errorf("unknown directive %s", dir)
	}

	if err := directive.Populate(args, kval); err != nil {
		return nil, err
	}

	return directive, nil
}

//msgp:ignore {TypeA} {TypeB}...
type IgnoreDirective struct {
	Types []string
}

func (i *IgnoreDirective) Populate(args []string, kwargs map[string]string) error {
	if len(kwargs) > 0 {
		return fmt.Errorf("invalid kwargs for ignore")
	}
	i.Types = args
	return nil
}

func (i IgnoreDirective) Build(tpset *structer.TypePackageSet, pkg string) (string, error) {
	ts := make([]string, len(i.Types))
	for idx, t := range i.Types {
		tn, err := structer.ParseLocalName(t, pkg)
		if err != nil {
			return "", errors.Wrapf(err, "ignore directive invalid type %s", t)
		}
		ln, err := tpset.LocalImportName(tn, pkg)
		if err != nil {
			return "", errors.Wrapf(err, "ignore directive invalid rel name %s, %s", tn, pkg)
		}
		ts[idx] = ln

	}
	return "//msgp:ignore " + strings.Join(ts, " "), nil
}

//msgp:tuple {TypeA} {TypeB}...
type TupleDirective struct {
	Types []string
}

func (i *TupleDirective) Populate(args []string, kwargs map[string]string) error {
	if len(kwargs) > 0 {
		return errors.Errorf("invalid kwargs for tuple")
	}
	i.Types = args
	return nil
}

func (i TupleDirective) Build(tpset *structer.TypePackageSet, pkg string) (string, error) {
	ts := make([]string, len(i.Types))
	for idx, t := range i.Types {
		tn, err := structer.ParseLocalName(t, pkg)
		if err != nil {
			return "", errors.Wrapf(err, "tuple directive invalid type %s", t)
		}
		ln, err := tpset.LocalImportName(tn, pkg)
		if err != nil {
			return "", errors.Wrapf(err, "tuple directive invalid rel name %s, %s", tn, pkg)
		}
		ts[idx] = ln
	}
	return "//msgp:tuple " + strings.Join(ts, " "), nil
}

//msgp:allowextra {TypeA} {TypeB}...
type AllowExtraDirective struct {
	Types []string
}

func (i *AllowExtraDirective) Populate(args []string, kwargs map[string]string) error {
	if len(kwargs) > 0 {
		return errors.Errorf("invalid kwargs for allowextra")
	}
	i.Types = args
	return nil
}

func (i AllowExtraDirective) Build(tpset *structer.TypePackageSet, pkg string) (string, error) {
	ts := make([]string, len(i.Types))
	for idx, t := range i.Types {
		tn, err := structer.ParseLocalName(t, pkg)
		if err != nil {
			return "", errors.Wrapf(err, "allowextra directive invalid type %s", t)
		}
		ln, err := tpset.LocalImportName(tn, pkg)
		if err != nil {
			return "", errors.Wrapf(err, "allowextra directive invalid rel name %s, %s", tn, pkg)
		}
		ts[idx] = ln
	}
	return "//msgp:allowextra " + strings.Join(ts, " "), nil
}

//msgp:shim {Type} using:{Func}
type InterceptDirective struct {
	Type  string
	Using string
}

func (i InterceptDirective) Build(tpset *structer.TypePackageSet, pkg string) (string, error) {
	tn, err := structer.ParseLocalName(i.Type, pkg)
	if err != nil {
		return "", err
	}
	ln, err := tpset.LocalImportName(tn, pkg)
	if err != nil {
		return "", errors.Wrapf(err, "intercept directive invalid rel name %s, %s", tn, pkg)
	}

	return fmt.Sprintf("//msgp:intercept %s using:%s", ln, i.Using), nil
}

func (i *InterceptDirective) Populate(args []string, kwargs map[string]string) error {
	var ok bool

	{ // type
		if len(args) != 1 {
			return errors.Errorf("invalid intercept directive - expected one arg, found %d", len(args))
		}
		i.Type = args[0]
	}

	{ // as
		if i.Using, ok = kwargs["using"]; !ok {
			return errors.Errorf("missing using: in intercept")
		}
		delete(kwargs, "using")
	}

	if len(kwargs) > 0 {
		return errors.Errorf("unknown keys")
	}

	return nil
}

//msgp:shim {Type} as:{Newtype} using:{toFunc/fromFunc} mode:convert
type ShimDirective struct {
	Type     string
	As       string
	ToFunc   string
	FromFunc string
	Mode     ShimMode
}

func (i ShimDirective) Build(tpset *structer.TypePackageSet, pkg string) (string, error) {
	tn, err := structer.ParseLocalName(i.Type, pkg)
	if err != nil {
		return "", errors.Wrapf(err, "shim directive invalid type %s", i.Type)
	}

	ln, err := tpset.LocalImportName(tn, pkg)
	if err != nil {
		return "", errors.Wrapf(err, "shim directive invalid rel name %s, %s", tn, pkg)
	}

	return fmt.Sprintf(
		"//msgp:shim %s as:%s using:%s/%s mode:%s",
		ln,
		i.As,
		i.ToFunc,
		i.FromFunc,
		i.Mode,
	), nil
}

func (i *ShimDirective) Populate(args []string, kwargs map[string]string) error {
	var ok bool

	{ // type
		if len(args) != 1 {
			return errors.Errorf("invalid shim directive - expected one arg, found %d", len(args))
		}
		i.Type = args[0]
	}

	{ // as
		if i.As, ok = kwargs["as"]; !ok {
			return errors.Errorf("missing as: in shim")
		}
		delete(kwargs, "as")
	}

	{ // mode
		if _, ok := kwargs["mode"]; !ok {
			i.Mode = Cast
		} else {
			sm := ShimMode(kwargs["mode"])
			if _, ok := ShimModes[sm]; !ok {
				return errors.Errorf("unknown shim mode %s", kwargs["mode"])
			}
			i.Mode = sm
		}
		delete(kwargs, "mode")
	}

	{ // using
		if _, ok := kwargs["using"]; !ok {
			return errors.Errorf("missing using: in shim")
		}
		methods := strings.Split(kwargs["using"], "/")
		if len(methods) != 2 {
			return errors.Errorf("expected 2 using::{} methods; found %d (%q)", len(methods), kwargs["mode"])
		}
		i.ToFunc = methods[0]
		i.FromFunc = methods[1]

		delete(kwargs, "using")
	}

	if len(kwargs) > 0 {
		return errors.Errorf("unknown keys")
	}

	return nil
}
