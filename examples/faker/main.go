package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	faker "github.com/jaswdr/faker/v2"
	fakerpb "go.cadenya.com/mcp-grpc-gateway/examples/faker/fakerpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const fakerPackagePath = "github.com/jaswdr/faker/v2"

var durationType = reflect.TypeOf(time.Duration(0))

func main() {
	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	server := grpc.NewServer()
	fakerpb.RegisterFakerServiceServer(server, newFakerServer())
	reflection.Register(server)

	log.Printf("faker gRPC server listening on %s", listener.Addr())
	if err := server.Serve(listener); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

type fakerServer struct {
	fakerpb.UnimplementedFakerServiceServer
	fakes  []fakeOption
	byName map[string]fakeOption
}

type fakeOption struct {
	name        string
	category    string
	description string
	method      reflect.Value
	args        []fakeArg
	variadic    bool
}

type fakeArg struct {
	name         string
	typ          string
	description  string
	goType       reflect.Type
	defaultValue any
	variadic     bool
}

type optionOverride struct {
	description string
	args        []argOverride
}

type argOverride struct {
	name         string
	description  string
	defaultValue any
}

var optionOverrides = map[string]optionOverride{
	"faker.int_between": {
		description: "Random integer between min and max.",
		args: []argOverride{
			{name: "min", description: "Minimum integer.", defaultValue: 1},
			{name: "max", description: "Maximum integer.", defaultValue: 100},
		},
	},
	"faker.int64_between": {
		description: "Random 64-bit integer between min and max.",
		args: []argOverride{
			{name: "min", description: "Minimum integer.", defaultValue: 1},
			{name: "max", description: "Maximum integer.", defaultValue: 100},
		},
	},
	"faker.float64": {
		description: "Random floating-point number with decimal precision between min and max.",
		args: []argOverride{
			{name: "decimals", description: "Maximum decimal places.", defaultValue: 2},
			{name: "min", description: "Minimum value.", defaultValue: 1},
			{name: "max", description: "Maximum value.", defaultValue: 100},
		},
	},
	"faker.random_number": {
		description: "Random number with the requested digit count.",
		args:        []argOverride{{name: "digits", description: "Number of digits.", defaultValue: 6}},
	},
	"faker.random_digit_not": {
		description: "Random digit excluding the ignored digits.",
		args:        []argOverride{{name: "ignore", description: "Digits to exclude.", defaultValue: []any{0}}},
	},
	"faker.random_string_element": {
		description: "Random string selected from values.",
		args:        []argOverride{{name: "values", description: "Candidate string values.", defaultValue: []any{"alpha", "bravo", "charlie"}}},
	},
	"faker.numerify": {
		description: "Replace # characters in a template with random digits.",
		args:        []argOverride{{name: "template", description: "Template containing # placeholders.", defaultValue: "ORD-####"}},
	},
	"faker.lexify": {
		description: "Replace ? characters in a template with random letters.",
		args:        []argOverride{{name: "template", description: "Template containing ? placeholders.", defaultValue: "??-??"}},
	},
	"faker.bothify": {
		description: "Replace ? and # placeholders with random letters and digits.",
		args:        []argOverride{{name: "template", description: "Template containing ? and # placeholders.", defaultValue: "??-####"}},
	},
	"lorem.words": {
		description: "Generate lorem words.",
		args:        []argOverride{{name: "count", description: "Number of words.", defaultValue: 3}},
	},
	"lorem.sentence": {
		description: "Generate a lorem sentence.",
		args:        []argOverride{{name: "count", description: "Number of words.", defaultValue: 8}},
	},
	"lorem.sentences": {
		description: "Generate lorem sentences.",
		args:        []argOverride{{name: "count", description: "Number of sentences.", defaultValue: 2}},
	},
	"lorem.paragraph": {
		description: "Generate a lorem paragraph.",
		args:        []argOverride{{name: "sentences", description: "Number of sentences.", defaultValue: 3}},
	},
	"lorem.paragraphs": {
		description: "Generate lorem paragraphs.",
		args:        []argOverride{{name: "count", description: "Number of paragraphs.", defaultValue: 2}},
	},
	"lorem.text": {
		description: "Generate lorem text with a maximum character count.",
		args:        []argOverride{{name: "max_chars", description: "Maximum characters.", defaultValue: 80}},
	},
	"programming_language.version": {
		description: "Generate a version string for a programming language.",
		args:        []argOverride{{name: "language", description: "Programming language name.", defaultValue: "Go"}},
	},
}

func newFakerServer() *fakerServer {
	fakes := discoverFakes()
	byName := make(map[string]fakeOption, len(fakes))
	for _, option := range fakes {
		byName[option.name] = option
	}
	return &fakerServer{fakes: fakes, byName: byName}
}

func (s *fakerServer) GetFakerOptions(_ context.Context, req *fakerpb.GetFakerOptionsRequest) (*fakerpb.GetFakerOptionsResponse, error) {
	filter := strings.ToLower(strings.TrimSpace(req.GetFilter()))
	resp := &fakerpb.GetFakerOptionsResponse{}
	for _, option := range s.fakes {
		if filter != "" && !option.matches(filter) {
			continue
		}
		resp.Options = append(resp.Options, option.toProto())
	}
	return resp, nil
}

func (s *fakerServer) GenerateFake(_ context.Context, req *fakerpb.GenerateFakeRequest) (*fakerpb.GenerateFakeResponse, error) {
	name := strings.TrimSpace(req.GetName())
	option, ok := s.byName[name]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported faker name %q", name)
	}
	value, err := option.generate(req.GetArgs().AsMap())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "generate %s: %v", name, err)
	}
	return &fakerpb.GenerateFakeResponse{Name: name, Value: value}, nil
}

func discoverFakes() []fakeOption {
	f := faker.New()
	fakerValue := reflect.ValueOf(f)
	var out []fakeOption

	out = append(out, discoverMethods("faker", fakerValue, true)...)
	for i := 0; i < fakerValue.NumMethod(); i++ {
		method := fakerValue.Type().Method(i)
		methodValue := fakerValue.Method(i)
		if !method.IsExported() || methodValue.Type().NumIn() != 0 || methodValue.Type().NumOut() != 1 {
			continue
		}
		returnType := methodValue.Type().Out(0)
		if !isProviderType(returnType) {
			continue
		}
		providerValue := methodValue.Call(nil)[0]
		category := toSnake(method.Name)
		out = append(out, discoverMethods(category, providerValue, false)...)
		if providerValue.Kind() == reflect.Struct {
			providerPointer := reflect.New(providerValue.Type())
			providerPointer.Elem().Set(providerValue)
			out = append(out, discoverMethods(category, providerPointer, false)...)
		}
	}

	out = dedupeOptions(out)
	sort.Slice(out, func(i, j int) bool {
		return out[i].name < out[j].name
	})
	return out
}

func dedupeOptions(options []fakeOption) []fakeOption {
	out := make([]fakeOption, 0, len(options))
	seen := map[string]struct{}{}
	for _, option := range options {
		if _, ok := seen[option.name]; ok {
			continue
		}
		seen[option.name] = struct{}{}
		out = append(out, option)
	}
	return out
}

func discoverMethods(category string, receiver reflect.Value, skipProviders bool) []fakeOption {
	var out []fakeOption
	for i := 0; i < receiver.NumMethod(); i++ {
		method := receiver.Type().Method(i)
		methodValue := receiver.Method(i)
		if !method.IsExported() {
			continue
		}
		if skipProviders && methodValue.Type().NumIn() == 0 && methodValue.Type().NumOut() == 1 && isProviderType(methodValue.Type().Out(0)) {
			continue
		}
		option, ok := fakeOptionForMethod(category, method.Name, methodValue)
		if ok {
			out = append(out, option)
		}
	}
	return out
}

func fakeOptionForMethod(category string, methodName string, method reflect.Value) (fakeOption, bool) {
	methodType := method.Type()
	if methodType.NumOut() == 0 {
		return fakeOption{}, false
	}
	for i := 0; i < methodType.NumOut(); i++ {
		if !isSerializableType(methodType.Out(i)) {
			return fakeOption{}, false
		}
	}
	args := make([]fakeArg, 0, methodType.NumIn())
	for i := 0; i < methodType.NumIn(); i++ {
		argType := methodType.In(i)
		variadic := methodType.IsVariadic() && i == methodType.NumIn()-1
		if variadic {
			argType = argType.Elem()
		}
		if !isSupportedArgType(argType) {
			return fakeOption{}, false
		}
		args = append(args, fakeArg{
			name:         fmt.Sprintf("arg%d", i+1),
			typ:          schemaType(argType, variadic),
			description:  fmt.Sprintf("Argument %d.", i+1),
			goType:       argType,
			defaultValue: defaultForType(argType, variadic),
			variadic:     variadic,
		})
	}

	name := category + "." + toSnake(methodName)
	option := fakeOption{
		name:        name,
		category:    category,
		description: defaultDescription(category, methodName),
		method:      method,
		args:        args,
		variadic:    methodType.IsVariadic(),
	}
	option.applyOverride()
	return option, true
}

func (o *fakeOption) applyOverride() {
	override, ok := optionOverrides[o.name]
	if !ok {
		return
	}
	if override.description != "" {
		o.description = override.description
	}
	for i := range override.args {
		if i >= len(o.args) {
			break
		}
		if override.args[i].name != "" {
			o.args[i].name = override.args[i].name
		}
		if override.args[i].description != "" {
			o.args[i].description = override.args[i].description
		}
		o.args[i].defaultValue = override.args[i].defaultValue
	}
}

func (o fakeOption) toProto() *fakerpb.FakerOption {
	args := make([]*fakerpb.FakerArgument, 0, len(o.args))
	for _, arg := range o.args {
		value, _ := structpb.NewValue(arg.defaultValue)
		args = append(args, &fakerpb.FakerArgument{
			Name:        arg.name,
			Type:        arg.typ,
			Description: arg.description,
			Default:     value,
		})
	}
	return &fakerpb.FakerOption{
		Name:        o.name,
		Category:    o.category,
		Description: o.description,
		Arguments:   args,
	}
}

func (o fakeOption) generate(input map[string]any) (value string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("faker panic: %v", r)
		}
	}()

	args := make([]reflect.Value, 0, len(o.args))
	for _, arg := range o.args {
		raw, ok := input[arg.name]
		if !ok {
			raw = arg.defaultValue
		}
		value, err := arg.reflectValue(raw)
		if err != nil {
			return "", err
		}
		if arg.variadic {
			for i := 0; i < value.Len(); i++ {
				args = append(args, value.Index(i))
			}
			continue
		}
		args = append(args, value)
	}

	results := o.method.Call(args)
	return encodeResults(results), nil
}

func (a fakeArg) reflectValue(raw any) (reflect.Value, error) {
	if a.variadic {
		items, ok := raw.([]any)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%s must be an array", a.name)
		}
		slice := reflect.MakeSlice(reflect.SliceOf(a.goType), 0, len(items))
		for _, item := range items {
			value, err := scalarReflectValue(a.goType, item)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("%s: %w", a.name, err)
			}
			slice = reflect.Append(slice, value)
		}
		return slice, nil
	}
	return scalarReflectValue(a.goType, raw)
}

func scalarReflectValue(t reflect.Type, raw any) (reflect.Value, error) {
	switch t.Kind() {
	case reflect.String:
		value, ok := raw.(string)
		if !ok {
			return reflect.Value{}, fmt.Errorf("expected string")
		}
		return reflect.ValueOf(value).Convert(t), nil
	case reflect.Bool:
		value, ok := raw.(bool)
		if !ok {
			return reflect.Value{}, fmt.Errorf("expected boolean")
		}
		return reflect.ValueOf(value).Convert(t), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := numberValue(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(int64(value)).Convert(t), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := numberValue(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		if value < 0 {
			return reflect.Value{}, fmt.Errorf("expected unsigned number")
		}
		return reflect.ValueOf(uint64(value)).Convert(t), nil
	case reflect.Float32, reflect.Float64:
		value, ok := raw.(float64)
		if !ok {
			return reflect.Value{}, fmt.Errorf("expected number")
		}
		return reflect.ValueOf(value).Convert(t), nil
	default:
		return reflect.Value{}, fmt.Errorf("unsupported argument type %s", t)
	}
}

func numberValue(raw any) (int64, error) {
	switch value := raw.(type) {
	case int:
		return int64(value), nil
	case int8:
		return int64(value), nil
	case int16:
		return int64(value), nil
	case int32:
		return int64(value), nil
	case int64:
		return value, nil
	case uint:
		return int64(value), nil
	case uint8:
		return int64(value), nil
	case uint16:
		return int64(value), nil
	case uint32:
		return int64(value), nil
	case uint64:
		return int64(value), nil
	case float64:
		return int64(value), nil
	default:
		return 0, fmt.Errorf("expected number")
	}
}

func encodeResults(results []reflect.Value) string {
	values := make([]any, 0, len(results))
	for _, result := range results {
		values = append(values, result.Interface())
	}
	if len(values) == 1 {
		if s, ok := values[0].(string); ok {
			return s
		}
		if d, ok := values[0].(time.Duration); ok {
			return d.String()
		}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return fmt.Sprint(values)
	}
	if len(values) == 1 {
		var single any
		if err := json.Unmarshal(raw, &single); err == nil {
			raw, _ = json.Marshal(single)
		}
	}
	return string(raw)
}

func (o fakeOption) matches(filter string) bool {
	if strings.Contains(strings.ToLower(o.name), filter) ||
		strings.Contains(strings.ToLower(o.category), filter) ||
		strings.Contains(strings.ToLower(o.description), filter) {
		return true
	}
	for _, arg := range o.args {
		if strings.Contains(strings.ToLower(arg.name), filter) ||
			strings.Contains(strings.ToLower(arg.description), filter) {
			return true
		}
	}
	return false
}

func isProviderType(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && t.PkgPath() == fakerPackagePath && t.Name() != "Faker"
}

func isSupportedArgType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func isSerializableType(t reflect.Type) bool {
	if t == durationType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.Array, reflect.Slice, reflect.Map, reflect.Struct:
		return true
	default:
		return false
	}
}

func schemaType(t reflect.Type, variadic bool) string {
	if variadic {
		return "array"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Float32, reflect.Float64:
		return "number"
	default:
		return "integer"
	}
}

func defaultForType(t reflect.Type, variadic bool) any {
	if variadic {
		switch t.Kind() {
		case reflect.String:
			return []any{"alpha", "bravo"}
		default:
			return []any{1}
		}
	}
	switch t.Kind() {
	case reflect.String:
		return "example"
	case reflect.Bool:
		return true
	case reflect.Float32, reflect.Float64:
		return 1.0
	default:
		return 1
	}
}

func defaultDescription(category string, methodName string) string {
	return fmt.Sprintf("Generate faker data using %s.%s.", category, toSnake(methodName))
}

func toSnake(value string) string {
	var out []rune
	for i, r := range value {
		if unicode.IsUpper(r) {
			if i > 0 {
				out = append(out, '_')
			}
			out = append(out, unicode.ToLower(r))
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
