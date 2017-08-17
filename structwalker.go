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
	depth      int
}

func newMsgpTypeVisitor(tpset *structer.TypePackageSet, typeQueue *TypeQueue) *msgpTypeVisitor {
	mtv := &msgpTypeVisitor{
		typeQueue: typeQueue,
		tpset:     tpset,
	}

	mtv.PartialTypeVisitor = structer.PartialTypeVisitor{
		EnterStructFunc: func(s structer.StructInfo) error {
			mtv.depth++
			return nil
		},
		LeaveStructFunc: func(s structer.StructInfo) error {
			mtv.depth++
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
