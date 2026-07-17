// Package template 提供不依赖环境或文件系统的模板解析、校验与渲染能力。
package template

import (
	"fmt"
	texttemplate "text/template"
	"text/template/parse"
)

var allowedFunctions = map[string]struct{}{
	"default": {},
	"eq":      {},
	"ne":      {},
	"and":     {},
	"or":      {},
	"not":     {},
}

// Template 是经过函数白名单校验的模板。
type Template struct {
	parsed *texttemplate.Template
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
