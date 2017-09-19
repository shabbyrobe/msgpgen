package msgpgen

import (
	"bytes"
	"fmt"
	"go/types"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"github.com/shabbyrobe/structer"
	"github.com/tinylib/msgp/gen"
)

type tplType struct {
	ID            int
	ImportName    string
	Pointer       bool
	Shim          *ShimDirective
	ShimPrimitive string
}

type tplVars struct {
	MapperType  string
	MapperVar   string
	OutType     string
	Interceptor string
	Types       []tplType
}

var replacePattern = regexp.MustCompile(`[/\.]`)

func genIntercept(pkg string, directives *Directives, state *State, iface *iface) (out *bytes.Buffer, intercept *InterceptDirective, err error) {
	tv := tplVars{}

	// Build types
	tv.Types = make([]tplType, 0, len(iface.types))
	for tn, typ := range iface.types {
		if !tn.IsExported() {
			continue
		}

		ptr := false
		if p, ok := typ.(*types.Pointer); ok {
			if tn, err = structer.ParseTypeName(p.Elem().String()); err != nil {
				return
			}
			ptr = true
		}

		if _, ok := directives.ignore[tn]; ok {
			continue
		}

		id, ok := state.Types[tn]
		if !ok {
			err = errors.Errorf("id not found for package %s, type %s, iface %s", pkg, tn.String(), iface.name)
			return
		}

		tt := tplType{
			Shim:       directives.shim[tn],
			ID:         id,
			ImportName: tn.ImportName(pkg, true),
			Pointer:    ptr,
		}
		if tt.Shim != nil {
			tt.ShimPrimitive = gen.Ident(tt.Shim.As).Value.String()
		}

		tv.Types = append(tv.Types, tt)
	}

	sort.Slice(tv.Types, func(i, j int) bool {
		return tv.Types[i].ID < tv.Types[j].ID
	})

	// Build mapper/interceptor type names
	tv.MapperType = iface.name.String()
	if len(tv.MapperType) == 0 {
		err = errors.Errorf("mapper name was empty for package %s, iface %s", pkg, iface.name)
		return
	}
	tv.MapperType = replacePattern.ReplaceAllString(tv.MapperType, "_")
	tv.MapperType = strings.Trim(tv.MapperType, "_")
	tv.MapperType = strings.ToLower(tv.MapperType[:1]) + tv.MapperType[1:]

	tv.MapperVar = fmt.Sprintf("%sInstance", tv.MapperType)
	tv.Interceptor = fmt.Sprintf("%sInterceptor", tv.MapperType)

	tv.OutType = iface.name.ImportName(pkg, true)

	var tpl *template.Template
	tpl, err = template.New("").Parse(interceptTpl)
	if err != nil {
		err = errors.Wrap(err, "mapper template parse failed")
		return
	}
	var buf bytes.Buffer
	if err = tpl.Execute(&buf, tv); err != nil {
		err = errors.Wrap(err, "mapper template exec failed")
		return
	}

	intercept = &InterceptDirective{Type: tv.OutType, Using: tv.Interceptor}
	out = &buf
	return
}

const interceptTpl = `
var {{.MapperVar}} = &{{.MapperType}}{}

func {{.Interceptor}}() *{{.MapperType}} { return {{.MapperVar}} }

type {{.MapperType}} struct {}

func (m *{{.MapperType}}) DecodeMsg(dc *msgp.Reader) (t {{.OutType}}, err error) {
	if dc.IsNil() {
		err = dc.ReadNil()
	} else {
		var sz uint32
		sz, err = dc.ReadArrayHeader()
		if err != nil {
			return
		}
		if sz != 2 {
			err = msgp.ArrayError{Wanted: 2, Got: sz}
			return
		}
		
		// Y U string? numbers are a minefield for client libraries in msgpack
		var s string
		var i int64
		s, err = dc.ReadString()
		if err != nil {
			return
		}
		i, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			return
		}

		switch i {
		{{- range .Types }}
		case {{.ID}}:
			{{- if .Shim }}
			var as {{.Shim.As}}
			if as, err = dc.Read{{.ShimPrimitive}}(); err != nil {
				return
			}

			{{- if (eq .Shim.Mode "convert") }}
			t, err = {{.Shim.FromFunc}}(as)
			{{- else }}
			t = {{.Shim.FromFunc}}(as)
			{{- end }}

			{{- else }}
			v := {{if .Pointer}}&{{end}}{{.ImportName}}{}
			if err = v.DecodeMsg(dc); err != nil {
				return
			}
			t = v
			{{- end }}
		{{- end }}
		default:
			err = fmt.Errorf("{{.OutType}}: unknown msg kind %d", i)
		}
	}
	return
}

func (m *{{.MapperType}}) UnmarshalMsg(bts []byte) (t {{.OutType}}, o []byte, err error) {
	o = bts
	if msgp.IsNil(bts) {
		o, err = msgp.ReadNilBytes(o)
	} else {
		var sz uint32
		sz, o, err = msgp.ReadArrayHeaderBytes(o)
		if err != nil {
			return
		}
		if sz != 2 {
			err = msgp.ArrayError{Wanted: 2, Got: sz}
			return
		}
		// Y U string? numbers are a minefield for client libraries in msgpack
		var s string
		var i int64
		s, o, err = msgp.ReadStringBytes(o)
		if err != nil {
			return
		}
		i, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			return
		}
		switch i {
		{{- range .Types }}
		case {{.ID}}:
			{{- if .Shim }}
			var as {{.Shim.As}}
			if as, o, err = msgp.Read{{.ShimPrimitive}}Bytes(o); err != nil {
				return
			}

			{{- if (eq .Shim.Mode "convert") }}
			t, err = {{.Shim.FromFunc}}(as)
			{{- else }}
			t = {{.Shim.FromFunc}}(as)
			{{- end }}

			{{- else }}
			v := {{if .Pointer}}&{{end}}{{.ImportName}}{}
			t = v
			o, err = v.UnmarshalMsg(o)
			{{- end }}

		{{- end }}
		default:
			err = fmt.Errorf("{{.OutType}}: unknown msg kind %d", i)
		}
	}
	return
}

func (m *{{.MapperType}}) EncodeMsg(t {{.OutType}}, en *msgp.Writer) (err error) {
	if t == nil {
		return en.WriteNil()
	}

	// array header, size 2
	err = en.Append(0x92)
	if err != nil {
		return err
	}

	switch t := t.(type) {
	{{- range .Types }}
	case {{ if .Pointer -}} * {{- end -}} {{.ImportName}}:
		if err = en.WriteString("{{.ID}}"); err != nil {
			return
		}

		{{- if .Shim }}
		{{- if (eq .Shim.Mode "convert") }}
		var tmp {{.ShimPrimitive}}
		if tmp, err = {{.Shim.ToFunc}}(t); err != nil {
			return
		}
		err = en.Write{{.ShimPrimitive}}(tmp)
		{{- else }}
		err = en.Write{{.ShimPrimitive}}({{.Shim.As}}(t))
		{{- end }}

		{{- else }}
		err = t.EncodeMsg(en)
		{{- end}}
	{{- end }}
	default:
		err = fmt.Errorf("{{.OutType}} unknown msg %T", t)
	}
	return
}

func (m *{{.MapperType}}) MarshalMsg(t {{.OutType}}, b []byte) (o []byte, err error) {
	o = b
	if t == nil {
		o = msgp.AppendNil(o)
		return
	}

	// array header, size 2
	o = append(o, 0x92)

	switch t := t.(type) {
	{{- range .Types }}
	case {{ if .Pointer -}} * {{- end -}} {{.ImportName}}:
		o = msgp.AppendString(o, "{{.ID}}")

		{{- if .Shim }}
		{{- if (eq .Shim.Mode "convert") }}
		var tmp {{.ShimPrimitive}}
		if tmp, err = {{.Shim.ToFunc}}(t); err != nil {
			return
		}
		o = msgp.Append{{.ShimPrimitive}}(o, tmp)
		{{- else }}
		o = msgp.Append{{.ShimPrimitive}}(o, {{.Shim.As}}(t))
		{{- end }}

		{{- else }}
		o, err = t.MarshalMsg(o)
		{{- end}}

	{{- end }}
	default:
		err = fmt.Errorf("{{.OutType}} unknown msg %T", t)
	}

	return
}

func (m *{{.MapperType}}) Msgsize(t {{.OutType}}) (s int) {
	switch t := t.(type) {
	case msgp.Sizer:
		return t.Msgsize()
	default:
		return msgp.GuessSize(t)
	}
}
`
