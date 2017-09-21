package msgpgen

import (
	"bytes"
	"fmt"
	"go/types"

	"github.com/shabbyrobe/structer"
)

type TypeQueue struct {
	contents []*TypeQueueItem

	// set of previously seen tp.key() values. this is used to prevent
	// duplicate additions.
	seenItems map[string]bool

	// set of previously seen tp.name values - this is used to collect
	// type names that we have considered, it does not prevent duplicate
	// additions.
	seenTypes map[string]bool

	tpset *structer.TypePackageSet
}

func NewTypeQueue(tpset *structer.TypePackageSet) *TypeQueue {
	return &TypeQueue{
		tpset:     tpset,
		seenItems: make(map[string]bool),
		seenTypes: make(map[string]bool),
	}
}

func (q *TypeQueue) AddObj(originPkg string, obj types.Object) *TypeQueueItem {
	var name string
	var typ types.Type
	if obj != nil {
		name = (obj.Pkg().Path() + "." + obj.Name())
		typ = obj.Type()
	} else {
		panic(nil)
	}
	return q.Add(originPkg, name, obj, typ)
}

func (q *TypeQueue) AddType(originPkg string, name string, typ types.Type) *TypeQueueItem {
	return q.Add(originPkg, name, nil, typ)
}

func (q *TypeQueue) Add(originPkg string, name string, obj types.Object, typ types.Type) *TypeQueueItem {
	if obj == nil {
		// we may still find nothing here - this is an ongoing pain point, mixing
		// types.Object and types.Type and the availability of each
		obj = q.tpset.ObjectByName(name)
	}

	q.seenTypes[name] = true

	var tqi *TypeQueueItem
	tqi = &TypeQueueItem{OriginPkg: originPkg, Name: name, Obj: obj, Type: typ}
	if _, ok := q.seenItems[tqi.Key()]; ok {
		return tqi
	}
	q.seenItems[tqi.Key()] = true
	q.contents = append(q.contents, tqi)
	return tqi
}

func (q *TypeQueue) Dequeue() *TypeQueueItem {
	if len(q.contents) == 0 {
		return nil
	}
	var tqi *TypeQueueItem
	tqi, q.contents = (q.contents)[0], (q.contents)[1:]
	return tqi
}

type TypeQueueItem struct {
	OriginPkg string
	Name      string
	Obj       types.Object
	Type      types.Type
	Parents   []types.Type
}

func (tqi *TypeQueueItem) Parent() types.Type {
	ln := len(tqi.Parents)
	if ln > 0 {
		return tqi.Parents[ln-1]
	}
	return nil
}

func (tqi *TypeQueueItem) SetParents(parents []types.Type) *TypeQueueItem {
	tqi.Parents = make([]types.Type, len(parents))
	copy(tqi.Parents, parents)
	return tqi
}

func (tqi *TypeQueueItem) ParentsString() string {
	var buf bytes.Buffer
	for i, parent := range tqi.Parents {
		if i > 0 {
			buf.WriteString(" -> ")
		}
		buf.WriteString(parent.String())
	}
	return buf.String()
}

func (tqi *TypeQueueItem) Key() string {
	return fmt.Sprintf("%s:%s", tqi.OriginPkg, tqi.Name)
}
