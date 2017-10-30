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
	queueItem  *TypeQueueItem
}

func newMsgpTypeVisitor(tpset *structer.TypePackageSet, typeQueue *TypeQueue) *msgpTypeVisitor {
	mtv := &msgpTypeVisitor{
		typeQueue: typeQueue,
		tpset:     tpset,
	}

	mtv.PartialTypeVisitor = structer.PartialTypeVisitor{}

	// need to track struct field stack to build these ignored messages
	// it outputs the nested struct as ignored if all its members are ignored.
	// test.go: Foo: Baz: ignored.
	// test.go: Foo: Qux: Ding: ignored.
	// test.go: Foo: Qux: Z: Dong: ignored.
	// test.go: Foo: Qux: Z: ignored.
	// test.go: Foo: Qux: ignored.
	mtv.PartialTypeVisitor.EnterStructFunc = func(ctx structer.WalkContext, s structer.StructInfo) error {
		return nil
	}
	mtv.PartialTypeVisitor.LeaveStructFunc = func(ctx structer.WalkContext, s structer.StructInfo) error {
		return nil
	}
	mtv.PartialTypeVisitor.EnterFieldFunc = func(ctx structer.WalkContext, s structer.StructInfo, field *types.Var, tag string) error {
		return nil
	}
	mtv.PartialTypeVisitor.LeaveFieldFunc = func(ctx structer.WalkContext, s structer.StructInfo, field *types.Var, tag string) error {
		return nil
	}

	mtv.PartialTypeVisitor.VisitBasicFunc = func(ctx structer.WalkContext, t *types.Basic) error {
		mtv.typeQueue.AddType(mtv.currentPkg, t.String(), t).SetParents(mtv.queueItem.Parents)
		return nil
	}

	mtv.PartialTypeVisitor.VisitNamedFunc = func(ctx structer.WalkContext, t *types.Named) error {
		mtv.typeQueue.AddType(mtv.currentPkg, t.String(), t).SetParents(mtv.queueItem.Parents)

		if isNamedCompoundType(t) {
			// Compound named types need to be walked as well, i.e.
			//   type Foos []Foo
			//   type FooMap map[Foo]Bar
			//   type FooChan chan Foo
			tn, err := structer.ParseTypeName(t.Underlying().String())
			if err != nil {
				return err
			}
			return structer.Walk(tn, t.Underlying(), mtv)
		}
		return nil
	}

	return mtv
}

func (t *msgpTypeVisitor) walk(name structer.TypeName, underlying types.Type, tqi *TypeQueueItem) error {
	t.currentPkg = name.PackagePath
	t.queueItem = tqi
	err := structer.Walk(name, underlying, t)
	t.currentPkg = ""
	t.queueItem = nil
	return err
}
