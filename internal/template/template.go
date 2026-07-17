// Package template 提供不依赖环境或文件系统的模板编译与渲染能力。
package template

import (
	"bytes"
	"fmt"
	"slices"
	texttemplate "text/template"
	"text/template/parse"

	"github.com/ghstlnx/dotfiles/internal/datakey"
)

var allowedFunctions = map[string]struct{}{
	"default": {},
	"eq":      {},
	"ne":      {},
	"and":     {},
	"or":      {},
	"not":     {},
}

var builtInVariables = map[string]struct{}{
	"OS":       {},
	"Arch":     {},
	"Hostname": {},
	"Profile":  {},
	"Home":     {},
}

// Template 是已经完成语法、函数与变量引用静态校验的模板。
type Template struct {
	parsed   *texttemplate.Template
	dataKeys []string
}

// Context 是渲染唯一可见的显式运行输入。Data 可以包含机器配置遗留键，Render 只暴露
// 编译时声明的键。
type Context struct {
	OS       string
	Arch     string
	Hostname string
	Profile  string
	Home     string
	Data     map[string]string
}

// Compile 解析 source，并检查 M1 函数白名单与 manifest 用户 data 声明。
func Compile(name string, source []byte, declaredData []string) (*Template, error) {
	declared, dataKeys, err := compileDataKeys(declaredData)
	if err != nil {
		return nil, fmt.Errorf("compile template %q: %w", name, err)
	}

	parsed, err := texttemplate.New(name).
		Option("missingkey=error").
		Funcs(texttemplate.FuncMap{"default": defaultString}).
		Parse(string(source))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", name, err)
	}
	for _, candidate := range parsed.Templates() {
		if candidate.Root == nil {
			continue
		}
		if err := walkNode(candidate.Root, func(node parse.Node) error {
			return validateNode(node, declared)
		}); err != nil {
			return nil, fmt.Errorf("template %q: %w", candidate.Name(), err)
		}
	}
	return &Template{parsed: parsed, dataKeys: dataKeys}, nil
}

// Render 使用显式 context 逐字节渲染模板。声明 data 缺值时拒绝渲染，不从 manifest
// default、进程环境或其他来源补值。
func (t *Template) Render(context Context) ([]byte, error) {
	values := map[string]string{
		"OS":       context.OS,
		"Arch":     context.Arch,
		"Hostname": context.Hostname,
		"Profile":  context.Profile,
		"Home":     context.Home,
	}
	for _, key := range t.dataKeys {
		value, exists := context.Data[key]
		if !exists {
			return nil, fmt.Errorf("declared data key %q is missing from render context; rerun init", key)
		}
		values[key] = value
	}

	var output bytes.Buffer
	if err := t.parsed.Execute(&output, values); err != nil {
		return nil, fmt.Errorf("render template %q: %w", t.parsed.Name(), err)
	}
	return output.Bytes(), nil
}

func compileDataKeys(dataKeys []string) (map[string]struct{}, []string, error) {
	declared := make(map[string]struct{}, len(dataKeys))
	keys := make([]string, 0, len(dataKeys))
	for _, key := range dataKeys {
		if !datakey.Valid(key) {
			return nil, nil, fmt.Errorf("declared data key %q is invalid", key)
		}
		if _, exists := declared[key]; exists {
			continue
		}
		declared[key] = struct{}{}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return declared, keys, nil
}

func defaultString(fallback, value string) string {
	if value == "" {
		return fallback
	}
	return value
}

func walkNode(node parse.Node, visit func(parse.Node) error) error {
	if node == nil {
		return nil
	}

	var err error
	switch current := node.(type) {
	case *parse.ListNode:
		for _, child := range current.Nodes {
			if err = walkNode(child, visit); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		err = walkNode(current.Pipe, visit)
	case *parse.IfNode:
		err = walkBranch(current.Pipe, current.List, current.ElseList, visit)
	case *parse.RangeNode:
		err = walkBranch(current.Pipe, current.List, current.ElseList, visit)
	case *parse.WithNode:
		err = walkBranch(current.Pipe, current.List, current.ElseList, visit)
	case *parse.TemplateNode:
		if current.Pipe != nil {
			err = walkNode(current.Pipe, visit)
		}
	case *parse.PipeNode:
		for _, command := range current.Cmds {
			if err = walkNode(command, visit); err != nil {
				return err
			}
		}
	case *parse.CommandNode:
		for _, argument := range current.Args {
			if err = walkNode(argument, visit); err != nil {
				return err
			}
		}
	case *parse.ChainNode:
		err = walkNode(current.Node, visit)
	case *parse.TextNode,
		*parse.CommentNode,
		*parse.BoolNode,
		*parse.NumberNode,
		*parse.StringNode,
		*parse.NilNode,
		*parse.DotNode,
		*parse.FieldNode,
		*parse.VariableNode,
		*parse.IdentifierNode,
		*parse.BreakNode,
		*parse.ContinueNode:
		// 叶子节点由 visit 校验。
	default:
		return fmt.Errorf("unsupported template AST node %T", node)
	}
	if err != nil {
		return err
	}
	return visit(node)
}

func walkBranch(
	pipe *parse.PipeNode,
	list, elseList *parse.ListNode,
	visit func(parse.Node) error,
) error {
	if err := walkNode(pipe, visit); err != nil {
		return err
	}
	if err := walkNode(list, visit); err != nil {
		return err
	}
	if elseList != nil {
		return walkNode(elseList, visit)
	}
	return nil
}

func validateNode(node parse.Node, declared map[string]struct{}) error {
	switch current := node.(type) {
	case *parse.IdentifierNode:
		if _, allowed := allowedFunctions[current.Ident]; !allowed {
			return fmt.Errorf("function %q is not allowed", current.Ident)
		}
	case *parse.FieldNode:
		if len(current.Ident) != 1 {
			return fmt.Errorf("variable reference %q must name one root value", current.String())
		}
		return validateRootVariable(current.Ident[0], declared)
	case *parse.VariableNode:
		if len(current.Ident) <= 1 {
			return nil
		}
		if current.Ident[0] != "$" || len(current.Ident) != 2 {
			return fmt.Errorf("variable reference %q must name one root value", current.String())
		}
		return validateRootVariable(current.Ident[1], declared)
	case *parse.ChainNode:
		if len(current.Field) != 0 {
			return fmt.Errorf("variable reference %q must name one root value", current.String())
		}
	}
	return nil
}

func validateRootVariable(name string, declared map[string]struct{}) error {
	if _, builtIn := builtInVariables[name]; builtIn {
		return nil
	}
	if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
		return fmt.Errorf("unknown built-in variable %q", "."+name)
	}
	if !datakey.Valid(name) {
		return fmt.Errorf("invalid user variable %q", "."+name)
	}
	if _, exists := declared[name]; !exists {
		return fmt.Errorf("user variable %q is not declared by manifest data", "."+name)
	}
	return nil
}
