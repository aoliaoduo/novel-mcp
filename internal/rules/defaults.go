package rules

// SystemDefaults 返回当前 MCP 内置的机械规则基线。
// 这些约束完全由服务端确定，不读取 HOME、cwd 或项目外配置。
func SystemDefaults() Structured {
	return Structured{
		ForbiddenPhrases: []string{"某种程度上", "值得注意的是", "不知为何", "五味杂陈"},
		FatigueWords: map[string]int{
			"不禁": 1, "竟然": 1, "仿佛": 2, "此外": 1, "然而": 2,
			"一丝": 2, "一抹": 2, "一缕": 2, "宛如": 1, "不由得": 1,
			"像一": 3, "沉默了": 2, "没有说话": 2, "几息": 3, "一息": 3, "数息": 2,
		},
	}
}
