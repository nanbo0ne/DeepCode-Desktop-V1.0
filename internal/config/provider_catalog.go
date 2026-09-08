package config

import "strings"

// ProviderPreset is one curated provider identity. Subscription and metered
// endpoints intentionally have distinct IDs and key slots even when they expose
// the same model names.
type ProviderPreset struct {
	ID          string
	Label       string
	Description string
	Category    string
	AccountURL  string
	Trusted     bool
	Entry       ProviderEntry
}

// ProviderPresetCatalog preserves all known official identities for existing
// settings views, canonicalization, and pricing. DeepSeek is first so callers
// that use the catalog as a default naturally prefer it.
func ProviderPresetCatalog() []ProviderPreset {
	return []ProviderPreset{
		deepSeekPreset(),
		preset("openai", "OpenAI", "OpenAI 官方 API", "global", "https://platform.openai.com/api-keys", "https://api.openai.com/v1", "OPENAI_API_KEY", "gpt-5.4"),
		anthropicPreset(),
		preset("openrouter", "OpenRouter", "可信多模型聚合平台", "global", "https://openrouter.ai/settings/keys", "https://openrouter.ai/api/v1", "OPENROUTER_API_KEY", "openai/gpt-5.4"),

		preset("dashscope", "阿里云百炼", "DashScope OpenAI 兼容接口", "china", "https://bailian.console.aliyun.com/", "https://dashscope.aliyuncs.com/compatible-mode/v1", "DASHSCOPE_API_KEY", "qwen3-max", "qwen3-coder-plus"),
		preset("zhipu", "智谱 BigModel", "智谱开放平台按量 API", "china", "https://open.bigmodel.cn/usercenter/apikeys", "https://open.bigmodel.cn/api/paas/v4", "ZHIPU_API_KEY", "glm-5", "glm-4.7"),
		preset("moonshot", "Kimi / Moonshot", "Moonshot 国内官方 API", "china", "https://platform.moonshot.cn/console/api-keys", "https://api.moonshot.cn/v1", "MOONSHOT_API_KEY", "kimi-k2.5"),
		preset("minimax", "MiniMax", "MiniMax 国内按量 API", "china", "https://platform.minimaxi.com/user-center/basic-information/interface-key", "https://api.minimaxi.com/v1", "MINIMAX_API_KEY", "MiniMax-M2.5"),
		preset("volcengine-ark", "火山方舟", "火山引擎方舟按量 API", "china", "https://console.volcengine.com/ark/region:ark+cn-beijing/apikey", "https://ark.cn-beijing.volces.com/api/v3", "ARK_API_KEY", "doubao-seed-2.0-pro"),
		preset("baidu-qianfan", "百度千帆", "百度千帆 OpenAI 兼容 API", "china", "https://console.bce.baidu.com/qianfan/ais/console/applicationConsole/application", "https://qianfan.baidubce.com/v2", "QIANFAN_API_KEY", "ernie-5.0"),
		preset("tencent-tokenhub", "腾讯混元 / TokenHub", "腾讯 MaaS TokenHub", "china", "https://console.cloud.tencent.com/hunyuan/start", "https://tokenhub.tencentmaas.com/v1", "TENCENT_TOKENHUB_API_KEY", "hunyuan-turbos-latest"),
		preset("stepfun", "阶跃星辰", "StepFun 开放平台", "china", "https://platform.stepfun.com/interface-key", "https://api.stepfun.com/v1", "STEPFUN_API_KEY", "step-3.5-flash"),
		mimoAPIPreset(),
		preset("siliconflow", "SiliconFlow", "国内多模型云与聚合平台", "china", "https://cloud.siliconflow.cn/account/ak", "https://api.siliconflow.cn/v1", "SILICONFLOW_API_KEY", "Qwen/Qwen3.5-397B-A17B"),

		preset("mimo-token-plan", "MiMo Token Plan", "独立套餐端点，密钥不与 MiMo API 共用", "plan", "https://platform.xiaomimimo.com/", "https://token-plan-cn.xiaomimimo.com/v1", "MIMO_TOKEN_PLAN_API_KEY", "mimo-v2.5-pro"),
		preset("dashscope-coding", "阿里 Coding Plan", "阿里云 Coding Plan 专用端点", "plan", "https://bailian.console.aliyun.com/", "https://coding.dashscope.aliyuncs.com/v1", "DASHSCOPE_CODING_API_KEY", "qwen3-coder-plus"),
		preset("dashscope-token-plan", "阿里 Token Plan", "阿里云 Token Plan 专用端点", "plan", "https://bailian.console.aliyun.com/", "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "DASHSCOPE_TOKEN_PLAN_API_KEY", "qwen3-coder-plus"),
		preset("zhipu-coding", "GLM Coding Plan", "智谱 Coding Plan 专用端点", "plan", "https://open.bigmodel.cn/usercenter/apikeys", "https://open.bigmodel.cn/api/coding/paas/v4", "ZHIPU_CODING_API_KEY", "glm-5"),
		preset("minimax-token-plan", "MiniMax Token Plan", "独立套餐密钥，不与按量 API 共用", "plan", "https://platform.minimaxi.com/", "https://api.minimaxi.com/v1", "MINIMAX_TOKEN_PLAN_API_KEY", "MiniMax-M2.5"),
		preset("volcengine-coding", "火山 Coding Plan", "火山方舟 Coding Plan 专用端点", "plan", "https://console.volcengine.com/ark/region:ark+cn-beijing/apikey", "https://ark.cn-beijing.volces.com/api/coding/v3", "ARK_CODING_API_KEY", "ark-code-latest"),
		preset("step-plan", "Step Plan", "阶跃星辰套餐专用端点", "plan", "https://platform.stepfun.com/", "https://api.stepfun.com/step_plan/v1", "STEP_PLAN_API_KEY", "step-3.5-flash"),
	}
}

// SelectableProviderPresetCatalog is the catalog for adding a new official
// provider. A custom OpenAI-compatible provider is intentionally represented
// by the settings editor's custom flow rather than a curated preset.
func SelectableProviderPresetCatalog() []ProviderPreset {
	return []ProviderPreset{deepSeekPreset()}
}

func preset(id, label, description, category, accountURL, baseURL, keyEnv string, models ...string) ProviderPreset {
	entry := ProviderEntry{
		Name:          id,
		Kind:          "openai",
		BaseURL:       baseURL,
		Models:        append([]string(nil), models...),
		APIKeyEnv:     keyEnv,
		ContextWindow: 262_144,
	}
	if len(models) > 0 {
		entry.Model = models[0]
		entry.Default = models[0]
	}
	if strings.Contains(baseURL, ".cn") || strings.Contains(baseURL, "deepseek.com") || strings.Contains(baseURL, "xiaomimimo.com") {
		entry.NoProxy = true
	}
	return ProviderPreset{ID: id, Label: label, Description: description, Category: category, AccountURL: accountURL, Trusted: true, Entry: entry}
}

func anthropicPreset() ProviderPreset {
	p := preset("anthropic", "Anthropic", "Anthropic 官方 Messages API", "global", "https://console.anthropic.com/settings/keys", "https://api.anthropic.com", "ANTHROPIC_API_KEY", "claude-opus-4-6", "claude-sonnet-4-6")
	p.Entry.Kind = "anthropic"
	p.Entry.ReasoningProtocol = "anthropic"
	return p
}

func deepSeekPreset() ProviderPreset {
	p := preset("deepseek", "DeepSeek", "DeepSeek 官方按量 API", "china", "https://platform.deepseek.com/api_keys", "https://api.deepseek.com", "DEEPSEEK_API_KEY", "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp")
	p.Entry.BalanceURL = "https://api.deepseek.com/user/balance"
	p.Entry.ContextWindow = 1_000_000
	return p
}

func mimoAPIPreset() ProviderPreset {
	p := preset("mimo-api", "Xiaomi MiMo API", "小米 MiMo 官方按量 API", "china", "https://platform.xiaomimimo.com/", "https://api.xiaomimimo.com/v1", "MIMO_API_KEY", "mimo-v2.5", "mimo-v2.5-pro")
	p.Entry.Model = "mimo-v2.5-pro"
	p.Entry.Default = "mimo-v2.5-pro"
	return p
}

func ProviderPresetByID(id string) (ProviderPreset, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range ProviderPresetCatalog() {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderPreset{}, false
}
