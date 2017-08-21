package msgpgen

import (
	"go/types"

	"github.com/shabbyrobe/structer"
)

type msgpTypeVisitor struct {
	structer.PartialTypeVisitor

	currentPkg string
	tpset      *structer.TypePackageSet
	typeQueue  *TypeQueue
}

func newMsgpTypeVisitor(tpset *structer.TypePackageSet, typeQueue *TypeQueue) *msgpTypeVisitor {
	mtv := &msgpTypeVisitor{
		typeQueue: typeQueue,
		tpset:     tpset,
	}

	mtv.PartialTypeVisitor = structer.PartialTypeVisitor{
		// need to track struct field stack to build these ignored messages
		// it outputs the nested struct as ignored if all its members are ignored.
		// test.go: Foo: Baz: ignored.
		// test.go: Foo: Qux: Ding: ignored.
		// test.go: Foo: Qux: Z: Dong: ignored.
		// test.go: Foo: Qux: Z: ignored.
		// test.go: Foo: Qux: ignored.
		EnterStructFunc: func(s structer.StructInfo) error {
			return nil
		},
		LeaveStructFunc: func(s structer.StructInfo) error {
			return nil
		},
		EnterFieldFunc: func(s structer.StructInfo, field *types.Var, tag string) error {
			return nil
		},
		LeaveFieldFunc: func(s structer.StructInfo, field *types.Var, tag string) error {
			return nil
		},

		VisitBasicFunc: func(t *types.Basic) error {
			mtv.typeQueue.AddType(mtv.currentPkg, t.String(), t)
			return nil
		},
		VisitNamedFunc: func(t *types.Named) error {
			mtv.typeQueue.AddType(mtv.currentPkg, t.String(), t)
			return nil
		},
	}
	return mtv
}
