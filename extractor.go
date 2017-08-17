package msgpgen

import (
	"fmt"
	"go/types"

	"github.com/shabbyrobe/structer"
)

type extractor struct {
	typq      *TypeQueue
	tpset     *structer.TypePackageSet
	tvis      *msgpTypeVisitor
	dctvCache *DirectivesCache

	// temporary file output mapped by package name, to be joined by newlines.
	tempOutput map[string][]string

	// have we rendered this type to the temp output? this is different to
	// the type queue's "seen" map as that includes the origin package too.
	tempRendered map[string]bool
}

func newExtractor(tpset *structer.TypePackageSet, dctvCache *DirectivesCache, typq *TypeQueue) *extractor {
	return &extractor{
		typq:         typq,
		tpset:        tpset,
		tvis:         newMsgpTypeVisitor(tpset, typq),
		dctvCache:    dctvCache,
		tempOutput:   make(map[string][]string),
		tempRendered: make(map[string]bool),
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

	// the package that uses the type is responsible for declaring //msgp:shim, // not the package that declares it, so we need to look at the referring
	// package's directives, not the declaration's.
	if _, ok := originDctvs.shim[tqi.Type.String()]; ok {
		fmt.Printf("%s: ALREADY SHIMMED\n", tqi.Name)
		return nil
	}

	// the package that declares the type is responsible for declaring //msgp:ignore,
	// not the package that refers to it, so we need to look at the package's directives,
	// not the origin's.
	if e.dctvCache.Ignored(pkgDctvs, tqi.Name) {
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
	tn, err := structer.ParseTypeName(ft.String())
	if err != nil {
		return err
	}
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

	e.tempOutput[tqi.OriginPkg] = append(e.tempOutput[tqi.OriginPkg],
		ShimDirective{
			Type:     importedName,
			As:       ft.Underlying().String(),
			ToFunc:   ft.Underlying().String(),
			FromFunc: importedName,
			Mode:     Cast,
		}.String(),
		IgnoreDirective{Types: []string{importedName}}.String(),
	)

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

			if s, ok := ft.Underlying().(*types.Struct); ok {
				if err := e.extractNamedStruct(tqi, pkg, ft, s); err != nil {
					return err
				}

			} else if _, ok := primitives[ft.Underlying().String()]; ok && ft.Underlying().String() != "interface{}" {
				if err := e.extractShimmedSupported(tqi, pkg, ft); err != nil {
					return err
				}

			} else {
				panic(fmt.Errorf("named unsupported type '%s', underlying '%s', originating '%s'", ft, ft.Underlying(), tqi.OriginPkg))
			}

		case *types.Basic:
			if ft.Underlying().String() == "rune" {
				// FIXME: Rune hacks abound. see newDirectives() for more details.
				fmt.Println("RUNES ARE A BIT OF A HACK")
				continue
			}

			// we should not see Basic types here - it should be caught further up
			// when we check the directly supported primitives.
			panic(fmt.Errorf("unsupported basic type: %s %T", ft.Underlying(), ft))

		default:
			panic(fmt.Errorf("main unsupported type: %s %T", ft.Underlying(), ft))
		}
	}

	return nil
}
