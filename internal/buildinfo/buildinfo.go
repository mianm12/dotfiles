// Package buildinfo 保存构建时注入并由 version 命令展示的元数据。
package buildinfo

// Version、Commit 和 BuildTime 可由构建过程通过 -ldflags -X 注入。
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info 描述当前 dot 二进制的构建来源。
type Info struct {
	Version   string
	Commit    string
	BuildTime string
}

// Current 返回规范化后的构建元数据；空值会替换为相应默认值。
func Current() Info {
	return Info{
		Version:   valueOr(Version, "dev"),
		Commit:    valueOr(Commit, "unknown"),
		BuildTime: valueOr(BuildTime, "unknown"),
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
