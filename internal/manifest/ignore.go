package manifest

import (
	"fmt"
	"strings"
)

// ignorePattern 是已经校验的 ignore 规则。basename 规则可在任意层级匹配；其余规则
// 都从模块根开始匹配。
type ignorePattern struct {
	segments      []string
	basename      bool
	directoryOnly bool
}

func parseIgnorePattern(pattern string) (ignorePattern, error) {
	if pattern == "" || strings.ContainsRune(pattern, '\x00') {
		return ignorePattern{}, fmt.Errorf("ignore pattern %q must not be empty or contain NUL", pattern)
	}
	if strings.HasPrefix(pattern, "!") {
		return ignorePattern{}, fmt.Errorf("ignore pattern %q uses unsupported negation", pattern)
	}
	if strings.ContainsAny(pattern, `?[]\`) {
		return ignorePattern{}, fmt.Errorf("ignore pattern %q uses unsupported glob syntax", pattern)
	}

	rooted := strings.HasPrefix(pattern, "/")
	directoryOnly := strings.HasSuffix(pattern, "/")
	trimmed := strings.TrimPrefix(pattern, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return ignorePattern{}, fmt.Errorf("ignore pattern %q has no path component", pattern)
	}

	segments := strings.Split(trimmed, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return ignorePattern{}, fmt.Errorf("ignore pattern %q contains an invalid path segment", pattern)
		}
		if strings.Contains(segment, "**") && segment != "**" {
			return ignorePattern{}, fmt.Errorf("ignore pattern %q requires ** to occupy a complete path segment", pattern)
		}
	}

	return ignorePattern{
		segments:      segments,
		basename:      !rooted && len(segments) == 1,
		directoryOnly: directoryOnly,
	}, nil
}

// matches 报告规范化模块相对路径是否被规则忽略。path 必须使用 / 分隔且不带首尾 /。
// 规则命中目录时，其全部后代也命中。
func (p ignorePattern) matches(path string, isDir bool) bool {
	pathSegments := strings.Split(path, "/")
	if !validMatchPath(pathSegments) {
		return false
	}

	for end := 1; end <= len(pathSegments); end++ {
		candidateIsDir := end < len(pathSegments) || isDir
		if p.directoryOnly && !candidateIsDir {
			continue
		}

		candidate := pathSegments[:end]
		if p.basename {
			if matchIgnoreSegment(p.segments[0], candidate[len(candidate)-1]) {
				return true
			}
			continue
		}
		if matchIgnoreSegments(p.segments, candidate) {
			return true
		}
	}
	return false
}

func validMatchPath(segments []string) bool {
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func matchIgnoreSegments(pattern, path []string) bool {
	type position struct {
		pattern int
		path    int
	}
	memo := make(map[position]bool)
	visited := make(map[position]bool)

	var match func(patternIndex, pathIndex int) bool
	match = func(patternIndex, pathIndex int) bool {
		current := position{pattern: patternIndex, path: pathIndex}
		if visited[current] {
			return memo[current]
		}
		visited[current] = true

		matched := false
		switch {
		case patternIndex == len(pattern):
			matched = pathIndex == len(path)
		case pattern[patternIndex] == "**":
			matched = match(patternIndex+1, pathIndex) ||
				(pathIndex < len(path) && match(patternIndex, pathIndex+1))
		case pathIndex < len(path) && matchIgnoreSegment(pattern[patternIndex], path[pathIndex]):
			matched = match(patternIndex+1, pathIndex+1)
		}
		memo[current] = matched
		return matched
	}

	return match(0, 0)
}

func matchIgnoreSegment(pattern, value string) bool {
	patternIndex := 0
	valueIndex := 0
	starIndex := -1
	starValueIndex := 0

	for valueIndex < len(value) {
		switch {
		case patternIndex < len(pattern) && pattern[patternIndex] == value[valueIndex]:
			patternIndex++
			valueIndex++
		case patternIndex < len(pattern) && pattern[patternIndex] == '*':
			starIndex = patternIndex
			patternIndex++
			starValueIndex = valueIndex
		case starIndex >= 0:
			patternIndex = starIndex + 1
			starValueIndex++
			valueIndex = starValueIndex
		default:
			return false
		}
	}
	for patternIndex < len(pattern) && pattern[patternIndex] == '*' {
		patternIndex++
	}
	return patternIndex == len(pattern)
}
