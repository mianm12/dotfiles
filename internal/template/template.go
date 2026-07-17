// Package template 提供不依赖环境或文件系统的模板解析、校验与渲染能力。
package template

import (
	"bytes"
	"fmt"
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

// Template 是经过函数白名单校验的模板。
type Template struct {
	parsed *texttemplate.Template
}

// Context 是渲染唯一可见的显式运行输入。Data 可以包含机器配置遗留键，Render 只暴露
// 当前 manifest 声明的键。
type Context struct {
	OS       string
	Arch     string
	Hostname string
	Profile  string
	Home     string
	Data     map[string]string
}

// Parse 解析 source，并拒绝 M1 配置语言未开放的函数。
func Parse(name string, source []byte) (*Template, error) {
	parsed, err := texttemplate.New(name).
		Option("missingkey=error").
		Funcs(texttemplate.FuncMap{"default": defaultString}).
		Parse(string(source))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", name, err)
	}
	for _, candidate := range parsed.Templates() {
		if err := validateFunctionNodes(candidate.Root); err != nil {
			return nil, fmt.Errorf("template %q: %w", candidate.Name(), err)
		}
	}
	return &Template{parsed: parsed}, nil
}

// ValidateVariables 检查所有根变量引用是否属于内建命名空间或 manifest 声明的用户 data。
func (t *Template) ValidateVariables(declaredData []string) error {
	declared := make(map[string]struct{}, len(declaredData))
	for _, key := range declaredData {
		if !datakey.Valid(key) {
			return fmt.Errorf("declared data key %q is invalid", key)
		}
		declared[key] = struct{}{}
	}
	for _, candidate := range t.parsed.Templates() {
		if err := validateVariableNodes(candidate.Root, declared); err != nil {
			return fmt.Errorf("template %q: %w", candidate.Name(), err)
		}
	}
	return nil
}

// Render 使用显式 context 逐字节渲染模板。declaredData 缺值时拒绝渲染，不从 manifest
// default、进程环境或其他来源补值。
func (t *Template) Render(declaredData []string, context Context) ([]byte, error) {
	if err := t.ValidateVariables(declaredData); err != nil {
		return nil, err
	}
	values := map[string]string{
		"OS":       context.OS,
		"Arch":     context.Arch,
		"Hostname": context.Hostname,
		"Profile":  context.Profile,
		"Home":     context.Home,
	}
	for _, key := range declaredData {
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

func defaultString(fallback, value string) string {
	if value == "" {
		return fallback
	}
	return value
}

func validateFunctionNodes(node parse.Node) error {
	if node == nil {
		return nil
	}
	switch current := node.(type) {
	case *parse.ListNode:
		for _, child := range current.Nodes {
			if err := validateFunctionNodes(child); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		return validateFunctionNodes(current.Pipe)
	case *parse.IfNode:
		return validateBranchNode(current.Pipe, current.List, current.ElseList)
	case *parse.RangeNode:
		return validateBranchNode(current.Pipe, current.List, current.ElseList)
	case *parse.WithNode:
		return validateBranchNode(current.Pipe, current.List, current.ElseList)
	case *parse.TemplateNode:
		return validateFunctionNodes(current.Pipe)
	case *parse.PipeNode:
		for _, command := range current.Cmds {
			if err := validateFunctionNodes(command); err != nil {
				return err
			}
		}
	case *parse.CommandNode:
		for _, argument := range current.Args {
			if err := validateFunctionNodes(argument); err != nil {
				return err
			}
		}
	case *parse.IdentifierNode:
		if _, allowed := allowedFunctions[current.Ident]; !allowed {
			return fmt.Errorf("function %q is not allowed", current.Ident)
		}
	}
	return nil
}

func validateBranchNode(pipe *parse.PipeNode, list, elseList *parse.ListNode) error {
	if err := validateFunctionNodes(pipe); err != nil {
		return err
	}
	if err := validateFunctionNodes(list); err != nil {
		return err
	}
	if elseList != nil {
		return validateFunctionNodes(elseList)
	}
	return nil
}

func validateVariableNodes(node parse.Node, declared map[string]struct{}) error {
	if node == nil {
		return nil
	}
	switch current := node.(type) {
	case *parse.ListNode:
		for _, child := range current.Nodes {
			if err := validateVariableNodes(child, declared); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		return validateVariableNodes(current.Pipe, declared)
	case *parse.IfNode:
		return validateVariableBranch(current.Pipe, current.List, current.ElseList, declared)
	case *parse.RangeNode:
		return validateVariableBranch(current.Pipe, current.List, current.ElseList, declared)
	case *parse.WithNode:
		return validateVariableBranch(current.Pipe, current.List, current.ElseList, declared)
	case *parse.TemplateNode:
		return validateVariableNodes(current.Pipe, declared)
	case *parse.PipeNode:
		for _, command := range current.Cmds {
			if err := validateVariableNodes(command, declared); err != nil {
				return err
			}
		}
	case *parse.CommandNode:
		for _, argument := range current.Args {
			if err := validateVariableNodes(argument, declared); err != nil {
				return err
			}
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
		return validateVariableNodes(current.Node, declared)
	}
	return nil
}

func validateVariableBranch(
	pipe *parse.PipeNode,
	list, elseList *parse.ListNode,
	declared map[string]struct{},
) error {
	if err := validateVariableNodes(pipe, declared); err != nil {
		return err
	}
	if err := validateVariableNodes(list, declared); err != nil {
		return err
	}
	if elseList != nil {
		return validateVariableNodes(elseList, declared)
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
