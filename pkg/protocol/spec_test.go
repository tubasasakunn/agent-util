package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// openRPCDoc は docs/openrpc.json をパースするための最小スキーマ。
type openRPCDoc struct {
	OpenRPC string `json:"openrpc"`
	Info    struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Methods    []openRPCMethod `json:"methods"`
	Components struct {
		Schemas map[string]openRPCSchema `json:"schemas"`
	} `json:"components"`
}

type openRPCMethod struct {
	Name   string                `json:"name"`
	Params []openRPCContentDescr `json:"params"`
	Result *openRPCContentDescr  `json:"result"`
}

type openRPCContentDescr struct {
	Name     string         `json:"name"`
	Required bool           `json:"required"`
	Schema   map[string]any `json:"schema"`
}

type openRPCSchema struct {
	Type       string         `json:"type"`
	Required   []string       `json:"required"`
	Properties map[string]any `json:"properties"`
}

// loadOpenRPC は docs/openrpc.json を読み込む。
func loadOpenRPC(t *testing.T) *openRPCDoc {
	t.Helper()
	// pkg/protocol からの相対パス。
	path := filepath.Join("..", "..", "docs", "openrpc.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read openrpc.json: %v", err)
	}
	var doc openRPCDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse openrpc.json: %v", err)
	}
	return &doc
}

// expectedMethods は methods.go で定義された全メソッドの集合。
func expectedMethods() []string {
	return []string{
		MethodAgentRun,
		MethodAgentAbort,
		MethodAgentConfigure,
		MethodToolRegister,
		MethodToolExecute,
		MethodMCPRegister,
		MethodGuardRegister,
		MethodGuardExecute,
		MethodVerifierRegister,
		MethodVerifierExecute,
		MethodStreamDelta,
		MethodStreamEnd,
		MethodContextStatus,
		MethodJudgeRegister,
		MethodJudgeEvaluate,
		MethodLLMExecute,
	}
}

func TestOpenRPC_MetadataMatches(t *testing.T) {
	doc := loadOpenRPC(t)
	if doc.OpenRPC == "" {
		t.Error("openrpc field is empty")
	}
	if doc.Info.Title != "ai-agent JSON-RPC API" {
		t.Errorf("info.title = %q, want %q", doc.Info.Title, "ai-agent JSON-RPC API")
	}
	if doc.Info.Version == "" {
		t.Error("info.version is empty")
	}
}

func TestOpenRPC_MethodNamesMatch(t *testing.T) {
	doc := loadOpenRPC(t)

	got := make(map[string]bool, len(doc.Methods))
	for _, m := range doc.Methods {
		got[m.Name] = true
	}
	want := expectedMethods()

	var missing []string
	for _, name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("methods missing from openrpc.json: %v", missing)
	}

	wantSet := make(map[string]bool, len(want))
	for _, n := range want {
		wantSet[n] = true
	}
	var extra []string
	for name := range got {
		if !wantSet[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		t.Errorf("methods present in openrpc.json but not in methods.go: %v", extra)
	}
}

// methodSchemaName は各メソッドの params 型名。
func methodSchemaName() map[string]string {
	return map[string]string{
		MethodAgentRun:         "AgentRunParams",
		MethodAgentAbort:       "AgentAbortParams",
		MethodAgentConfigure:   "AgentConfigureParams",
		MethodToolRegister:     "ToolRegisterParams",
		MethodToolExecute:      "ToolExecuteParams",
		MethodMCPRegister:      "MCPRegisterParams",
		MethodGuardRegister:    "GuardRegisterParams",
		MethodGuardExecute:     "GuardExecuteParams",
		MethodVerifierRegister: "VerifierRegisterParams",
		MethodVerifierExecute:  "VerifierExecuteParams",
		MethodStreamDelta:      "StreamDeltaParams",
		MethodStreamEnd:        "StreamEndParams",
		MethodContextStatus:    "ContextStatusParams",
		MethodJudgeRegister:    "JudgeRegisterParams",
		MethodJudgeEvaluate:    "JudgeEvaluateParams",
		MethodLLMExecute:       "LLMExecuteParams",
	}
}

// goTypes は params 型名 → reflect.Type の対応。
func goTypes() map[string]reflect.Type {
	return map[string]reflect.Type{
		"AgentRunParams":         reflect.TypeOf(AgentRunParams{}),
		"AgentAbortParams":       reflect.TypeOf(AgentAbortParams{}),
		"AgentConfigureParams":   reflect.TypeOf(AgentConfigureParams{}),
		"ToolRegisterParams":     reflect.TypeOf(ToolRegisterParams{}),
		"ToolExecuteParams":      reflect.TypeOf(ToolExecuteParams{}),
		"MCPRegisterParams":      reflect.TypeOf(MCPRegisterParams{}),
		"GuardRegisterParams":    reflect.TypeOf(GuardRegisterParams{}),
		"GuardExecuteParams":     reflect.TypeOf(GuardExecuteParams{}),
		"VerifierRegisterParams": reflect.TypeOf(VerifierRegisterParams{}),
		"VerifierExecuteParams":  reflect.TypeOf(VerifierExecuteParams{}),
		"StreamDeltaParams":      reflect.TypeOf(StreamDeltaParams{}),
		"StreamEndParams":        reflect.TypeOf(StreamEndParams{}),
		"ContextStatusParams":    reflect.TypeOf(ContextStatusParams{}),
		"JudgeRegisterParams":    reflect.TypeOf(JudgeRegisterParams{}),
		"JudgeEvaluateParams":    reflect.TypeOf(JudgeEvaluateParams{}),
		"LLMExecuteParams":       reflect.TypeOf(LLMExecuteParams{}),
	}
}

// goRequiredFields は struct から JSON タグを抽出し、omitempty が付かず非ポインタなフィールドを必須として返す。
func goRequiredFields(t reflect.Type) []string {
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		if name == "" {
			continue
		}
		hasOmitempty := false
		for _, p := range parts[1:] {
			if p == "omitempty" {
				hasOmitempty = true
				break
			}
		}
		if hasOmitempty {
			continue
		}
		if f.Type.Kind() == reflect.Ptr {
			continue
		}
		required = append(required, name)
	}
	sort.Strings(required)
	return required
}

func TestOpenRPC_RequiredFieldsMatch(t *testing.T) {
	doc := loadOpenRPC(t)

	docMethods := make(map[string]openRPCMethod, len(doc.Methods))
	for _, m := range doc.Methods {
		docMethods[m.Name] = m
	}

	schemas := doc.Components.Schemas
	types := goTypes()
	mapping := methodSchemaName()

	for _, method := range expectedMethods() {
		t.Run(method, func(t *testing.T) {
			schemaName, ok := mapping[method]
			if !ok {
				t.Fatalf("no params schema mapping for %s", method)
			}
			gt, ok := types[schemaName]
			if !ok {
				t.Fatalf("no Go type for schema %s", schemaName)
			}
			s, ok := schemas[schemaName]
			if !ok {
				t.Fatalf("schema %s not found in openrpc.json", schemaName)
			}

			goReq := goRequiredFields(gt)
			docReq := append([]string(nil), s.Required...)
			sort.Strings(docReq)

			if !reflect.DeepEqual(goReq, docReq) {
				t.Errorf("required fields mismatch for %s:\n  Go:      %v\n  OpenRPC: %v", schemaName, goReq, docReq)
			}

			// すべての Go フィールド（必須 / オプション）が schema.properties にも存在することを確認。
			goAll := goAllJSONFieldNames(gt)
			for _, name := range goAll {
				if _, ok := s.Properties[name]; !ok {
					t.Errorf("schema %s missing property %q", schemaName, name)
				}
			}
		})
	}
}

// goAllJSONFieldNames は struct の全 JSON フィールド名を返す。
func goAllJSONFieldNames(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestOpenRPC_SchemaFilesExist(t *testing.T) {
	dir := filepath.Join("..", "..", "docs", "schemas")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read schemas dir: %v", err)
	}
	got := make(map[string]bool, len(entries))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			got[strings.TrimSuffix(e.Name(), ".json")] = true
		}
	}
	for name := range goTypes() {
		if !got[name] {
			t.Errorf("schema file missing: docs/schemas/%s.json", name)
		}
	}
}
