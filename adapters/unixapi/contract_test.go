package unixapi

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// The operator API's contract, docs/reference/operator-api.json, is
// checked here against the server: the routes, the fields of every answer
// and request, and the values of every enum. The panel checks the same file
// from its side (panel/tests/operator_contract.rs).

const contractPath = "../../docs/reference/operator-api.json"

// contractTypes names the Go type behind each schema's x-go-type.
var contractTypes = map[string]reflect.Type{}

func init() {
	for _, value := range []any{
		domain.BotSnapshot{}, domain.TaskSnapshot{}, domain.ExecutionSnapshot{}, domain.NodeSnapshot{}, domain.Event{},
		domain.ApprovalWait{}, domain.ApprovalsSnapshot{}, domain.EventsSnapshot{}, domain.EmitRuleSnapshot{}, domain.Artifact{}, domain.BudgetsSnapshot{},
		domain.CorrelationRecord{}, domain.DelegationGrant{}, domain.DelegationSnapshot{}, domain.ModelPolicySnapshot{},
		domain.NodeCapabilities{}, domain.NotifyTargetSnapshot{}, domain.PendingDecision{}, domain.PlacementSnapshot{},
		domain.PolicyRecord{}, domain.Result{}, domain.TaskLineage{}, domain.ToolCall{}, domain.ToolRuleSnapshot{},
		domain.ToolRulesSnapshot{}, domain.TriggerRef{}, domain.TriggerRefSnapshot{}, domain.TriggerSnapshot{}, domain.Usage{},
		application.Overview{}, application.BotDefinition{}, application.BotPatch{}, application.Group{}, application.GroupMember{},
		application.BudgetsDefinition{}, application.ModelPolicyDefinition{},
		SubmitRequest{}, TaskRequest{}, SubmitResponse{}, RunResponse{}, CancelResponse{}, ReportResponse{},
		DecideRequest{}, MessageRequest{}, application.SentTaskMessage{}, StopResponse{}, application.StopRefusal{}, EnrollTokenRequest{}, EnrollTokenResponse{}, RevokeResponse{}, FireResponse{}, ErrorResponse{},
	} {
		t := reflect.TypeOf(value)
		contractTypes[t.String()] = t
	}
}

type contractSchema struct {
	Ref                  string                     `json:"$ref"`
	Type                 string                     `json:"type"`
	GoType               string                     `json:"x-go-type"`
	GoEnum               string                     `json:"x-go-enum"`
	Enum                 []string                   `json:"enum"`
	Properties           map[string]*contractSchema `json:"properties"`
	Required             []string                   `json:"required"`
	Items                *contractSchema            `json:"items"`
	AdditionalProperties *contractSchema            `json:"additionalProperties"`
}

type contract struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]*contractSchema `json:"schemas"`
	} `json:"components"`
}

func loadContract(t *testing.T) contract {
	t.Helper()
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec contract
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("%s: %v", contractPath, err)
	}
	return spec
}

func (c contract) resolve(s *contractSchema) *contractSchema {
	for s != nil && s.Ref != "" {
		s = c.Components.Schemas[strings.TrimPrefix(s.Ref, "#/components/schemas/")]
	}
	return s
}

// TestContractRoutesAreTheServers: every route the operator address serves
// is in the contract, and the contract names no other.
func TestContractRoutesAreTheServers(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	served := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || (fn.Name.Name != "operatorRoutes" && fn.Name.Name != "OperatorHandler") {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "HandleFunc" {
				return true
			}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok {
				route, _ := strconv.Unquote(lit.Value)
				served[route] = true
			}
			return true
		})
	}
	if len(served) < 20 {
		t.Fatalf("read %d operator routes from server.go; the parse is wrong", len(served))
	}
	described := map[string]bool{}
	for path, operations := range loadContract(t).Paths {
		for method := range operations {
			if method == "parameters" {
				continue
			}
			described[strings.ToUpper(method)+" "+path] = true
		}
	}
	for route := range served {
		if !described[route] {
			t.Errorf("the server serves %q and the contract does not describe it", route)
		}
	}
	for route := range described {
		if !served[route] {
			t.Errorf("the contract describes %q and the server does not serve it", route)
		}
	}
}

// TestContractSchemasAreTheGoTypes: every schema with an x-go-type has the
// JSON fields of that type, the same required ones (no omitempty, no
// pointer) and the same kind of value in each.
func TestContractSchemasAreTheGoTypes(t *testing.T) {
	spec := loadContract(t)
	checked := 0
	for name, schema := range spec.Components.Schemas {
		if schema.GoType == "" {
			continue
		}
		goType, ok := contractTypes[schema.GoType]
		if !ok {
			t.Errorf("%s: x-go-type %q is not in contractTypes", name, schema.GoType)
			continue
		}
		checked++
		fields := jsonFields(goType)
		for field, f := range fields {
			property, ok := schema.Properties[field]
			if !ok {
				t.Errorf("%s: the Go type has %q and the contract does not", name, field)
				continue
			}
			if want, got := jsonKind(f.Type), spec.resolve(property).Type; want != got {
				t.Errorf("%s.%s: Go writes a %s, the contract says %s", name, field, want, got)
			}
		}
		for field := range schema.Properties {
			if _, ok := fields[field]; !ok {
				t.Errorf("%s: the contract has %q and the Go type does not", name, field)
			}
		}
		var required []string
		for field, f := range fields {
			if !f.optional {
				required = append(required, field)
			}
		}
		sort.Strings(required)
		described := slices.Clone(schema.Required)
		sort.Strings(described)
		if !slices.Equal(required, described) {
			t.Errorf("%s: required fields are %v in Go, %v in the contract", name, required, described)
		}
	}
	if checked < 30 {
		t.Fatalf("checked %d schemas; expected the contract's thirty-odd", checked)
	}
}

// TestContractEnumsAreTheDomains: every schema with an x-go-enum lists
// exactly the string constants of that type in the domain package.
func TestContractEnumsAreTheDomains(t *testing.T) {
	constants := domainConstants(t)
	checked := 0
	for name, schema := range loadContract(t).Components.Schemas {
		if schema.GoEnum == "" {
			continue
		}
		checked++
		goName := strings.TrimPrefix(schema.GoEnum, "domain.")
		want := constants[goName]
		if len(want) == 0 {
			t.Errorf("%s: no string constant of type %s in domain", name, schema.GoEnum)
			continue
		}
		got := slices.Clone(schema.Enum)
		sort.Strings(want)
		sort.Strings(got)
		if !slices.Equal(want, got) {
			t.Errorf("%s: the domain has %q, the contract lists %q", name, want, got)
		}
	}
	if checked < 9 {
		t.Fatalf("checked %d enums, expected 9", checked)
	}
}

type jsonField struct {
	Type     reflect.Type
	optional bool
}

func jsonFields(t reflect.Type) map[string]jsonField {
	fields := map[string]jsonField{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if f.Anonymous && tag == "" {
			for name, inner := range jsonFields(f.Type) {
				fields[name] = inner
			}
			continue
		}
		if !f.IsExported() || tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		fields[name] = jsonField{Type: f.Type, optional: strings.Contains(options, "omitempty") || f.Type.Kind() == reflect.Pointer}
	}
	return fields
}

func jsonKind(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeOf(time.Time{}) {
		return "string"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct, reflect.Interface:
		return "object"
	}
	return t.Kind().String()
}

// domainConstants reads the string constants of each named type declared
// in the domain package's sources.
func domainConstants(t *testing.T) map[string][]string {
	t.Helper()
	sources, err := filepath.Glob("../../domain/*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string][]string{}
	fset := token.NewFileSet()
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, source, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value := spec.(*ast.ValueSpec)
				ident, ok := value.Type.(*ast.Ident)
				if !ok {
					continue
				}
				for _, v := range value.Values {
					if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						text, _ := strconv.Unquote(lit.Value)
						found[ident.Name] = append(found[ident.Name], text)
					}
				}
			}
		}
	}
	return found
}
