package msgpgen

import (
	"fmt"
	"go/types"

	"github.com/pkg/errors"
	"github.com/shabbyrobe/structer"
)

type extractor struct {
	typq      *TypeQueue
	tpset     *structer.TypePackageSet
	tvis      *msgpTypeVisitor
	dctvCache *DirectivesCache
	ifaces    ifaces
	state     *State

	// temporary file output mapped by package name, to be joined by newlines.
	tempOutput map[string][]string

	// extra file output mapped by package name, to be joined by newlines. this
	// goes into the result AFTER msgp has been run.
	extraOutput map[string][]string

	// have we rendered this type to the temp output? this is different to
	// the type queue's "seen" map as that includes the origin package too.
	tempRendered map[string]bool
}

func newExtractor(tpset *structer.TypePackageSet, dctvCache *DirectivesCache, typq *TypeQueue, state *State) *extractor {
	return &extractor{
		typq:         typq,
		tpset:        tpset,
		tvis:         newMsgpTypeVisitor(tpset, typq),
		dctvCache:    dctvCache,
		tempOutput:   make(map[string][]string),
		extraOutput:  make(map[string][]string),
		tempRendered: make(map[string]bool),
		state:        state,
		ifaces:       make(ifaces),
	}
}

func (e *extractor) extractNamedStruct(tqi *TypeQueueItem, pkg string, ft *types.Named, s *types.Struct) error {
	// type is a named struct. we need to walk all types nested in
	// this declaration and queue them for processing, and we also
	// need to extract the definition to write the msgpack generator temp
	// file.

	if !e.tempRendered[tqi.Name] {
		e.tempRendered[tqi.Name] = true
	} else {
		return nil
	}

	originDctvs, err := e.dctvCache.Ensure(tqi.OriginPkg)
	if err != nil {
		return err
	}

	pkgDctvs, err := e.dctvCache.Ensure(pkg)
	if err != nil {
		return err
	}

	tn, err := structer.ParseTypeName(ft.String())
	if err != nil {
		return err
	}

	// the package that uses the type is responsible for declaring //msgp:shim, // not the package that declares it, so we need to look at the referring
	// package's directives, not the declaration's.
	if _, ok := originDctvs.shim[tn]; ok {
		fmt.Printf("%s: ALREADY SHIMMED\n", tqi.Name)
		return nil
	}

	// the package that declares the type is responsible for declaring //msgp:ignore,
	// not the package that refers to it, so we need to look at the package's directives,
	// not the origin's.
	if e.dctvCache.Ignored(pkgDctvs, tn) {
		fmt.Printf("%s: IGNORING\n", tqi.Name)
		return nil
	}

	kind := e.tpset.Kinds[pkg]
	if kind != structer.UserPackage {
		// don't collect the type if it is in a vendored package.
		// This should probably be an error, you should probably shim in
		// this case.

		// FIXME: in the case of vendor packages, this could look up whether the
		// type supports the msgpack functions - we may have enough information.
		return fmt.Errorf("%s: type '%s' in %v package cannot be extracted - use a shim instead or write your own serialisation",
			tqi.OriginPkg, ft.String(), kind)
	}

	// walk structs looking for new types to queue
	e.tvis.currentPkg = pkg
	if err := structer.Walk(tn, ft.Underlying(), e.tvis); err != nil {
		return err
	}

	// build the output {{{
	fmt.Printf("%s: EXTRACTING\n", tqi.Name)
	contents, err := e.tpset.ExtractSource(tn)
	if err != nil {
		return err
	}

	pkgDctvs.add(&TupleDirective{
		Types: []string{findImportedName(tqi.Name, pkg)},
	})

	e.tempOutput[pkg] = append(e.tempOutput[pkg],
		"type "+string(contents))
	// }}}

	return nil
}

// type is declared to be a msgp supported type - we can shim it with a cast,
// but only if the underlying type isn't interface{}. If it is an interface,
// msgp probably needs a facility to do a type assertion using a shim but
// for now we have to spit chips and ask you to handle it yourself.
func (e *extractor) extractShimmedSupported(tqi *TypeQueueItem, pkg string, ft *types.Named) error {
	originRenderKey := tqi.OriginPkg + "/" + ft.String()
	if !e.tempRendered[originRenderKey] {
		e.tempRendered[originRenderKey] = true
	} else {
		return nil
	}

	fmt.Printf("%s: SHIMMING INTO %s\n", tqi.Name, tqi.OriginPkg)

	importedName := findImportedName(ft.String(), tqi.OriginPkg)

	shimDctv := &ShimDirective{
		Type:     ft.String(),
		As:       ft.Underlying().String(),
		ToFunc:   ft.Underlying().String(),
		FromFunc: importedName,
		Mode:     Cast,
	}

	dctvs, err := e.dctvCache.Ensure(tqi.OriginPkg)
	if err != nil {
		return err
	}

	// Add the shim directive, but don't add the ignore directive - we aren't actually
	// ignoring the type, we're just telling msgp not to raise errors about it.
	if err := dctvs.add(shimDctv); err != nil {
		return err
	}

	if kind, ok := e.tpset.Kinds[pkg]; ok {
		// Emit the actual type definition into the origin package
		if kind == structer.UserPackage && pkg == tqi.OriginPkg {
			tn, err := structer.ParseTypeName(ft.String())
			if err != nil {
				return err
			}
			contents, err := e.tpset.ExtractSource(tn)
			if err != nil {
				return err
			}
			e.tempOutput[tqi.OriginPkg] = append(e.tempOutput[tqi.OriginPkg],
				"type "+string(contents),
			)
		}
	}
	return nil
}

func (e *extractor) extract() error {
	for {
		tqi := e.typq.Dequeue()
		if tqi == nil {
			break
		}

		// If the field type is definitely supported by msgp we are golden
		if _, ok := primitives[tqi.Type.String()]; ok {
			// FIXME: Though maybe for whatever reason you might be shimming a
			// msgp primitive in a specific package?
			fmt.Printf("%s->%s: SUPPORTED DIRECTLY\n", tqi.OriginPkg, tqi.Name)
			continue
		}

		switch ft := tqi.Type.(type) {
		case *types.Named:
			pkg := ft.Obj().Pkg().Path()

			tn, err := structer.ParseTypeName(ft.String())
			if err != nil {
				return err
			}

			if s, ok := ft.Underlying().(*types.Struct); ok {
				if err := e.extractNamedStruct(tqi, pkg, ft, s); err != nil {
					return err
				}

			} else if e.isIntercepted(tqi.OriginPkg, tn) {
				// ignore for now, but eventually we can walk the list of implemented interfaces
				// to find types that implement the intercepted interface

			} else if _, ok := primitives[ft.Underlying().String()]; ok && !types.IsInterface(ft.Underlying()) {
				if err := e.extractShimmedSupported(tqi, pkg, ft); err != nil {
					return err
				}

			} else if isNamedCompoundType(ft) {
				// seems to work OK if we just do nothing here. i thought we might need to
				// extract the definition but I think that happens elsewhere.

			} else if types.IsInterface(ft) {
				if err := e.extractInterface(tqi, ft); err != nil {
					return err
				}

			} else {
				panic(fmt.Errorf("named unsupported type '%s', underlying '%s', originating '%s'", ft, ft.Underlying(), tqi.OriginPkg))
			}

		case *types.Basic:
			// we should not see Basic types here - it should be caught further up
			// when we check the directly supported primitives.
			panic(fmt.Errorf("unsupported basic type: %s %T, parents: %s", ft.Underlying(), ft, tqi.ParentsString()))

		default:
			panic(fmt.Errorf("main unsupported type: %s %T, parents: %s", ft.Underlying(), ft, tqi.ParentsString()))
		}
	}

	// build interface mappers
	for _, iface := range e.ifaces {
		for _, inPkg := range iface.inPackages {
			pkgDctvs, ok := e.dctvCache.pkgDirectives[inPkg]
			if !ok {
				return errors.Errorf("could not find directives for package %s", inPkg)
			}
			buf, interceptDctv, err := genIntercept(inPkg, pkgDctvs, e.state, iface)
			if err != nil {
				return err
			}
			pkgDctvs.add(interceptDctv)

			e.extraOutput[inPkg] = append(e.extraOutput[inPkg], buf.String())
		}
	}

	return nil
}

func (e *extractor) extractInterface(tqi *TypeQueueItem, typ types.Type) error {
	// FIXME: bail if we encounter interface{}

	if e.state == nil {
		return errors.Errorf("tried to extract interface %s without a state file", typ)
	}

	tn, err := structer.ParseTypeName(typ.String())
	if err != nil {
		return err
	}

	// Find the types that implement the interface and add them to the type queue for
	// walking, but only if we have not already done so for this interface
	if e.ifaces[tn] == nil {
		e.ifaces[tn] = newIface(tn)

		ts, err := e.tpset.FindImplementers(tn)
		if err != nil {
			return err
		}

		e.ifaces[tn].types = ts

		for ctn, ct := range ts {
			if !ctn.IsExported() {
				continue
			}

			// Every interface type needs a stable ID in the state file
			if _, err := e.state.EnsureType(ctn); err != nil {
				return err
			}

			// FIXME: double-copying the parents list here, but this is because
			// we need to add to it.
			parents := make([]types.Type, len(tqi.Parents)+1)
			for i, p := range tqi.Parents {
				parents[i] = p
			}
			parents[len(tqi.Parents)] = typ

			// If the interface is implemented by a pointer, unwrap it before
			// we queue it.
			var elem types.Type = ct
			if p, ok := ct.(*types.Pointer); ok {
				elem = p.Elem()
			}
			e.typq.AddType(tqi.OriginPkg, ctn.String(), elem).SetParents(parents)
		}
	}

	// Add the package that referenced this interface so we can emit code into it
	// for handling it.
	e.ifaces[tn].addPackage(tqi.OriginPkg)

	return nil
}

func (e *extractor) isIntercepted(origin string, tn structer.TypeName) bool {
	if e.tpset.Kinds[origin] == structer.UserPackage {
		originDctvs, err := e.dctvCache.Ensure(origin)
		if err != nil {
			panic(err)
		}

		_, ok := originDctvs.intercepted[tn]
		return ok
	}
	return false
}

type ifaces map[structer.TypeName]*iface

type iface struct {
	name       structer.TypeName
	types      map[structer.TypeName]types.Type
	inPackages []string
}

func newIface(tn structer.TypeName) *iface {
	return &iface{
		name:  tn,
		types: make(map[structer.TypeName]types.Type),
	}
}

func (i *iface) addPackage(pkg string) {
	i.inPackages = append(i.inPackages, pkg)
}

func isNamedCompoundType(t types.Type) bool {
	_, ok := t.(*types.Named)
	if !ok {
		return false
	}

	switch t.Underlying().(type) {
	case *types.Array:
		return true
	case *types.Slice:
		return true
	case *types.Map:
		return true
	case *types.Chan:
		return true
	}
	return false
}
