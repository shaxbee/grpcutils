package genelmtypes

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/davecgh/go-spew/spew"
	"github.com/gengo/grpc-gateway/protoc-gen-grpc-gateway/descriptor"
	pbdescriptor "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

type config struct {
	alwaysQualifyTypeNames bool
}

type ElmType interface {
	ElmType() string
	IsTypeAlias() bool
}
type NamedElmType interface {
	ElmType
	ElmTypeName() string
}

type simpleElmType string

func (s simpleElmType) ElmType() string   { return string(s) }
func (s simpleElmType) IsTypeAlias() bool { return false }

type repeatedElmType struct {
	t ElmType
}

func (r repeatedElmType) ElmType() string   { return fmt.Sprintf("List %s", r.t.ElmType()) }
func (r repeatedElmType) IsTypeAlias() bool { return false }

type namedElmType struct {
	Name string
	Type ElmType
}

func (t *namedElmType) ElmType() string {
	return t.Type.ElmType()
	return fmt.Sprintf("%s = %s", t.Name, t.Type.ElmType())
}
func (t *namedElmType) ElmTypeName() string {
	return t.Name
}
func (t *namedElmType) IsTypeAlias() bool { return t.Type.IsTypeAlias() }

type objectElmType struct {
	Fields []NamedElmType
}

func (t objectElmType) IsTypeAlias() bool { return true }

func (t *objectElmType) ElmType() string {
	fields := []string{}
	for _, f := range t.Fields {
		fields = append(fields, fmt.Sprintf("  %s: Maybe %s", f.ElmTypeName(), f.ElmType()))
	}
	return fmt.Sprintf("{\n%s\n}", strings.Join(fields, ",\n"))
}

func (cfg config) fqmnToType(fqmn string, registry *descriptor.Registry) (ElmType, error) {
	m, err := registry.LookupMsg("", fqmn)
	if err != nil {
		return nil, err
	}
	return cfg.messageToElmType(m, registry)
}

func (cfg config) fieldToType(f *descriptor.Field, reg *descriptor.Registry) (NamedElmType, error) {
	// FieldMessage
	var fieldType ElmType = simpleElmType("String")
	switch f.GetType() {
	case pbdescriptor.FieldDescriptorProto_TYPE_DOUBLE:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_FLOAT:
		fieldType = simpleElmType("Float")
	case pbdescriptor.FieldDescriptorProto_TYPE_INT64:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_UINT64:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_INT32:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_FIXED64:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_FIXED32:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_UINT32:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_SFIXED32:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_SFIXED64:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_SINT32:
		fallthrough
	case pbdescriptor.FieldDescriptorProto_TYPE_SINT64:
		fieldType = simpleElmType("Int")
	case pbdescriptor.FieldDescriptorProto_TYPE_BOOL:
		fieldType = simpleElmType("Boolean")
	case pbdescriptor.FieldDescriptorProto_TYPE_STRING:
		fieldType = simpleElmType("String")
	case pbdescriptor.FieldDescriptorProto_TYPE_GROUP:
		fieldType = simpleElmType("?") // ?
	case pbdescriptor.FieldDescriptorProto_TYPE_MESSAGE:
		// TODO: should resolve type name relative to this type
		ft, err := reg.LookupMsg("", f.GetTypeName())
		if err != nil {
			return nil, err
		}
		fieldType = simpleElmType(cfg.messageTypeName(ft))
	case pbdescriptor.FieldDescriptorProto_TYPE_BYTES:
		fieldType = simpleElmType("string") // could be more correct
	case pbdescriptor.FieldDescriptorProto_TYPE_ENUM:
		e, err := reg.LookupEnum("", f.GetTypeName())
		if err != nil {
			return nil, err
		}

		name := cfg.enumTypeName(e)
		fieldType = simpleElmType(name)
	default:
		spew.Dump("default:", f.GetType())
		fieldType = simpleElmType(fmt.Sprint(f.GetTypeName()))
	}
	if f.GetLabel() == pbdescriptor.FieldDescriptorProto_LABEL_REPEATED {
		fieldType = repeatedElmType{fieldType}
	}
	return &namedElmType{Name: f.GetName(), Type: fieldType}, nil
}

func (cfg config) messageToElmType(m *descriptor.Message, reg *descriptor.Registry) (ElmType, error) {
	t := &objectElmType{Fields: []NamedElmType{}}
	for _, f := range m.Fields {
		field, err := cfg.fieldToType(f, reg)
		if err != nil {
			return nil, err
		}
		t.Fields = append(t.Fields, field)
	}
	return &namedElmType{Name: cfg.messageTypeName(m), Type: t}, nil
}

func (cfg config) enumTypeName(e *descriptor.Enum) string {
	name := strings.Replace(e.FQEN(), ".", "", -1)
	if !cfg.alwaysQualifyTypeNames {
		if strings.HasPrefix(name, e.File.GoPkg.Name) {
			name = name[len(e.File.GoPkg.Name):]
		}
	}
	return name
}

func (cfg config) messageTypeName(m *descriptor.Message) string {
	name := strings.Replace(m.FQMN(), ".", "", -1)
	if !cfg.alwaysQualifyTypeNames {
		if strings.HasPrefix(name, m.File.GoPkg.Name) {
			name = name[len(m.File.GoPkg.Name):]
		}
	}
	return name
}

func (cfg config) enumToElmType(e *descriptor.Enum, reg *descriptor.Registry) (ElmType, error) {
	options := []string{}
	for _, v := range e.Value {
		options = append(options, fmt.Sprintf(`%s`, v.GetName()))
	}
	name := cfg.enumTypeName(e)
	return &namedElmType{
		Name: name,
		Type: simpleElmType(strings.Join(options, " | ")),
	}, nil
}

func generateElmTypes(file *descriptor.File, registry *descriptor.Registry, qualifyTypes bool) (string, error) {
	result := []ElmType{}
	f, err := registry.LookupFile(file.GetName())
	if err != nil {
		return "", err
	}
	cfg := config{
		alwaysQualifyTypeNames: qualifyTypes,
	}
	for _, enum := range f.Enums {
		t, err := cfg.enumToElmType(enum, registry)
		if err != nil {
			return "", err
		}
		result = append(result, t)
	}
	for _, message := range f.Messages {
		t, err := cfg.messageToElmType(message, registry)
		if err != nil {
			return "", err
		}
		result = append(result, t)
	}

	buf := new(bytes.Buffer)
	tmpl, err := template.New("").Parse("-- this is a generated file\n{{range .}}type {{if .IsTypeAlias}}alias {{end}}{{.ElmTypeName}} = {{.ElmType}}\n\n{{end}}")
	if err != nil {
		return "", err
	}
	err = tmpl.Execute(buf, result)
	if err != nil {
		return "", err
	}
	return string(buf.Bytes()), nil
}
