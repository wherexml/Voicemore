// Package providers knows the model providers of the app and turns the two
// default models of the settings into where a dictation connects.
//
// Speech recognition and text cleanup are independent. Each has a local
// service, configured in the settings since the first version; cloud
// providers from a small catalog; and providers the user added. Credentials
// are read through a Secrets function, which the app backs with its
// encrypted local credential store item; nothing here stores or logs one.
//
// What works is said plainly. Text cleanup goes to any provider with an
// OpenAI-compatible chat API: the local oMLX, DeepSeek, custom ones.
// Speech recognition goes to the local service and to custom ones the
// user declares compatible, through this app's own streaming protocol,
// and to Alibaba Cloud and Volcengine through their own protocols
// (internal/cloudasr), never through the local one. Only the models and
// access modes whose protocol the providers' documents state are offered:
// Usable says which, and why not for the others.
package providers

import (
	"errors"
	"net/url"
	"slices"
	"strings"

	"voiceinput/internal/asr"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/cloudasr"
	"voiceinput/internal/config"
)

const (
	KindASR     = "asr"
	KindCleanup = "cleanup"

	Local    = "local"
	Aliyun   = "aliyun"
	Volc     = "volc"
	DeepSeek = "deepseek"
	Sub2API  = "sub2api" // Voicemore Official; retain the persisted ID and credential account.

	DeepSeekEndpoint = "https://api.deepseek.com/v1"

	VolcNew    = "new"
	VolcLegacy = "legacy"
	VolcAgent  = "agent"
)

var (
	// ErrNotSet: no default model is chosen for the kind.
	ErrNotSet = errors.New("no default model is set")
	// ErrUnconfigured: the default's provider is off, lacks credentials or
	// an address, or no longer has the model.
	ErrUnconfigured = errors.New("the default model's provider is not configured")
	// ErrNoAdapter: the model, or the provider's access mode, has no
	// documented protocol this app implements.
	ErrNoAdapter = errors.New("this model or access mode is not supported in this version")
)

// Model is a model of a catalog provider.
type Model struct {
	ID          string
	Label       string // a text key, for models shown by a name rather than their ID
	Recommended bool
	Custom      bool // added by the user
}

// Entry is a provider of the catalog. Its models are those its own setup
// screens offer; whether the service serves them is not checked here.
type Entry struct {
	ID, Kind, Name string // Name is a text key
	Logo           string
	KeyURL         string
	DocsURL        string
	Models         []Model
}

var Catalog = []Entry{
	{ID: Sub2API, Kind: KindASR, Name: "pSub2API", Logo: "voicemore",
		KeyURL: cloudasr.Sub2APIEndpoint + "/keys", DocsURL: cloudasr.Sub2APIEndpoint,
		Models: []Model{{ID: cloudasr.Sub2APIModel, Label: "sub2apiModel", Recommended: true}}},
	{ID: Aliyun, Kind: KindASR, Name: "pAliyun", Logo: "alibabacloud",
		KeyURL: "https://bailian.console.aliyun.com/", DocsURL: "https://help.aliyun.com/zh/model-studio/",
		Models: []Model{{ID: "fun-asr-realtime", Recommended: true}, {ID: "qwen3-asr-flash-realtime"}}},
	{ID: Volc, Kind: KindASR, Name: "pVolc", Logo: "volcengine",
		KeyURL: "https://console.volcengine.com/", DocsURL: "https://www.volcengine.com/docs/",
		Models: []Model{{ID: "doubao-streaming-asr-2.0", Label: "volcPreset"}, {ID: "doubao-seed-asr-2-0"}}},
	{ID: DeepSeek, Kind: KindCleanup, Name: "pDeepseek", Logo: "deepseek",
		KeyURL: "https://platform.deepseek.com/", DocsURL: "https://api-docs.deepseek.com/",
		Models: []Model{{ID: "deepseek-flash", Recommended: true}, {ID: "deepseek-v4-pro"}}},
}

// Lookup finds a catalog provider.
func Lookup(id string) (Entry, bool) {
	for _, e := range Catalog {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// ModeUsable says the app implements a catalog provider's current access
// mode, and otherwise gives the text key of why not. Agent Plan uses the
// documented /plan/sauc endpoint and its dedicated key.
func ModeUsable(id string, s config.Settings) (bool, string) {
	return true, ""
}

// ModelUsable says the app implements the protocol of a catalog model, and
// otherwise gives the text key of why not.
func ModelUsable(id, model string) (bool, string) {
	if id == Aliyun && cloudasr.AliyunProtocol(model) == "" {
		// Not among the realtime models of Alibaba's documents.
		return false, "whyNoProtocol"
	}
	return true, ""
}

// AliyunBase is the Alibaba address of the settings, or the default.
func AliyunBase(s config.Settings) string {
	if b := strings.TrimSpace(s.Providers.AliyunBase); b != "" {
		return b
	}
	return cloudasr.AliyunDefaultBase
}

// VolcResource is the Volcengine resource ID of the settings.
func VolcResource(s config.Settings) string {
	switch s.Providers.VolcResource {
	case cloudasr.VolcConcurrent, cloudasr.VolcClassicHourly:
		return s.Providers.VolcResource
	}
	return cloudasr.VolcHourly
}

// Secrets returns the stored credential of an account, or "".
type Secrets func(account string) string

func ManagedVoice(s config.Settings) bool { return s.ASRAssign.Provider == Sub2API }
func Sub2APIBase(s config.Settings) string {
	if b := strings.TrimSpace(s.Providers.Sub2APIEndpoint); b != "" {
		return b
	}
	return cloudasr.Sub2APIEndpoint
}

// VolcMode is the Volcengine access mode of the settings.
func VolcMode(s config.Settings) string {
	switch s.Providers.VolcMode {
	case VolcLegacy, VolcAgent:
		return s.Providers.VolcMode
	}
	return VolcNew
}

// Fields are the credential fields a catalog provider needs, in its
// current mode. Each mode of Volcengine keeps its own.
func Fields(id string, s config.Settings) []string {
	if id == Volc && VolcMode(s) == VolcLegacy {
		return []string{"appId", "token"}
	}
	return []string{"apiKey"}
}

// Account is the encrypted local credential store account of a provider's credential field.
func Account(id string, s config.Settings, field string) string {
	if id == Volc {
		return "provider." + id + "." + VolcMode(s) + "." + field
	}
	return "provider." + id + "." + field
}

// CustomAccount is the encrypted local credential store account of a custom provider's API key.
func CustomAccount(id string) string { return "provider.custom." + id + ".apiKey" }

// Accounts lists every encrypted local credential store account the settings can refer to.
func Accounts(s config.Settings) []string {
	out := []string{Account(Aliyun, s, "apiKey"), Account(DeepSeek, s, "apiKey"),
		Account(Sub2API, s, "apiKey"),
		"provider.volc.new.apiKey", "provider.volc.legacy.appId", "provider.volc.legacy.token", "provider.volc.agent.apiKey"}
	for _, c := range s.Providers.Custom {
		out = append(out, CustomAccount(c.ID))
	}
	return out
}

// Enabled says a catalog provider is turned on.
func Enabled(id string, s config.Settings) bool { return s.Providers.Enabled[id] }

// Filled says every credential of a catalog provider is present.
func Filled(id string, s config.Settings, sec Secrets) bool {
	for _, f := range Fields(id, s) {
		if strings.TrimSpace(sec(Account(id, s, f))) == "" {
			return false
		}
	}
	return true
}

// Endpoint is the DeepSeek endpoint of the settings, or its default.
func Endpoint(s config.Settings) string {
	if e := strings.TrimSpace(s.Providers.DeepSeekEndpoint); e != "" {
		return e
	}
	return DeepSeekEndpoint
}

// ValidURL says a text is an http(s) address.
func ValidURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// APIBase is an endpoint without a trailing slash or /chat/completions,
// which the cleanup client adds itself.
func APIBase(endpoint string) string {
	return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(endpoint), "/"), "/chat/completions")
}

// ChatURL is the chat-completions address an endpoint leads to, with the
// suffix once: an endpoint that already ends with it is left as it is.
func ChatURL(endpoint string) string { return APIBase(endpoint) + "/chat/completions" }

// Configured says a catalog provider has what it needs. It says nothing
// about whether the credentials are right: only a test can.
func Configured(id string, s config.Settings, sec Secrets) bool {
	switch {
	case !Filled(id, s, sec):
		return false
	case id == DeepSeek:
		return ValidURL(Endpoint(s))
	case id == Aliyun:
		return ValidSocketURL(AliyunBase(s))
	case id == Sub2API:
		return (cloudasr.Sub2API{Base: Sub2APIBase(s)}).Check(nil) == nil
	}
	return true
}

// ValidSocketURL says a text is a ws(s) address.
func ValidSocketURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "ws" || u.Scheme == "wss") && u.Host != ""
}

// Models are a catalog provider's models, with those the user added.
func Models(id string, s config.Settings) []Model {
	e, _ := Lookup(id)
	out := slices.Clone(e.Models)
	if id == DeepSeek {
		for _, m := range s.Providers.DeepSeekModels {
			out = append(out, Model{ID: m, Custom: true})
		}
	}
	return out
}

// Custom finds a provider the user added.
func Custom(id string, s config.Settings) (config.CustomProvider, bool) {
	for _, c := range s.Providers.Custom {
		if c.ID == id {
			return c, true
		}
	}
	return config.CustomProvider{}, false
}

// Brand resolves known service branding without changing its ID or credentials.
func Brand(id string, s config.Settings) string {
	if c, ok := Custom(id, s); ok {
		u, err := url.Parse(strings.TrimSpace(c.URL))
		if err == nil {
			host := strings.ToLower(u.Hostname())
			if host == "dashscope.aliyuncs.com" || strings.HasSuffix(host, ".dashscope.aliyuncs.com") || host == "dashscope-intl.aliyuncs.com" || host == "dashscope-us.aliyuncs.com" || strings.HasSuffix(host, ".maas.aliyuncs.com") {
				return Aliyun
			}
		}
	}
	return id
}

// CustomReady says a custom provider is on, has a valid address and at
// least one model. Its key is optional.
func CustomReady(c config.CustomProvider) bool {
	return c.Enabled && ValidURL(c.URL) && len(c.Models) > 0
}

// LocalReady says the local service of a kind is set up.
func LocalReady(kind string, s config.Settings) bool {
	if kind == KindASR {
		return strings.TrimSpace(s.ASRURL) != ""
	}
	return strings.TrimSpace(s.CleanupURL) != "" && strings.TrimSpace(s.CleanupModel) != ""
}

// LocalModel is the model of the local service of a kind. For speech it
// is what the service last reported, which the app cannot choose.
func LocalModel(kind string, s config.Settings) string {
	if kind == KindASR {
		return s.ASRModel
	}
	return strings.TrimSpace(s.CleanupModel)
}

// Option is a model a default can be set to.
type Option struct {
	Provider string // Local, a catalog ID or a custom provider's ID
	Model    string
}

// Eligible lists the models the default of a kind can be: those of
// providers that are set up and that the app can actually use.
func Eligible(kind string, s config.Settings, sec Secrets) []Option {
	if kind == KindCleanup && ManagedVoice(s) {
		if Check(KindASR, s, sec) == nil {
			return []Option{{Sub2API, cloudasr.Sub2APIModel}}
		}
		return nil
	}
	var out []Option
	if LocalReady(kind, s) {
		out = append(out, Option{Local, LocalModel(kind, s)})
	}
	for _, e := range Catalog {
		if ok, _ := ModeUsable(e.ID, s); ok && e.Kind == kind && Enabled(e.ID, s) && Configured(e.ID, s, sec) {
			for _, m := range Models(e.ID, s) {
				if ok, _ := ModelUsable(e.ID, m.ID); ok {
					out = append(out, Option{e.ID, m.ID})
				}
			}
		}
	}
	for _, c := range s.Providers.Custom {
		if c.Kind == kind && CustomReady(c) {
			for _, m := range c.Models {
				out = append(out, Option{c.ID, m})
			}
		}
	}
	return out
}

// Assigned is the default of a kind.
func Assigned(kind string, s config.Settings) config.Assignment {
	if kind == KindASR {
		return s.ASRAssign
	}
	if ManagedVoice(s) {
		return config.Assignment{Provider: Sub2API, Model: cloudasr.Sub2APIModel}
	}
	return s.CleanupAssign
}

// Check says whether the default of a kind can serve: nil, ErrNotSet,
// ErrNoAdapter or ErrUnconfigured. A default that cannot serve is never
// replaced by another model.
func Check(kind string, s config.Settings, sec Secrets) error {
	a := Assigned(kind, s)
	if a.Provider == "" {
		return ErrNotSet
	}
	if _, ok := Lookup(a.Provider); ok {
		if ok, _ := ModeUsable(a.Provider, s); !ok {
			return ErrNoAdapter
		}
		if ok, _ := ModelUsable(a.Provider, a.Model); !ok {
			return ErrNoAdapter
		}
	}
	for _, o := range Eligible(kind, s, sec) {
		if o.Provider == a.Provider && (a.Provider == Local || o.Model == a.Model) {
			return nil
		}
	}
	return ErrUnconfigured
}

// ModelName is the model of the default of a kind, for display.
func ModelName(kind string, s config.Settings) string {
	a := Assigned(kind, s)
	if a.Provider == Local {
		return LocalModel(kind, s)
	}
	return a.Model
}

// ASR is the speech service the default model is on: the local protocol
// for the local service and compatible custom ones, the provider's own
// for Alibaba Cloud and Volcengine.
func ASR(s config.Settings, sec Secrets) (asr.Dialer, error) {
	if err := Check(KindASR, s, sec); err != nil {
		return nil, err
	}
	a := s.ASRAssign
	return Dialer(a.Provider, a.Model, s, sec), nil
}

// Dialer is the speech service of a provider and model, as configured.
func Dialer(provider, model string, s config.Settings, sec Secrets) asr.Dialer {
	switch provider {
	case Sub2API:
		return cloudasr.Sub2API{Base: Sub2APIBase(s), APIKey: sec(Account(Sub2API, s, "apiKey")), TextOptions: &cloudasr.VoiceTextOptions{Version: 1, Format: s.TextStyle, PolishRules: s.PolishRules}}
	case Local:
		// Only the local service takes the speech language; custom
		// services and the cloud keep their own routing and defaults.
		return asr.LocalLanguage(s.ASRURL, config.LocalASRLanguage(s.ASRLanguage))
	case Aliyun:
		return cloudasr.Aliyun{Base: AliyunBase(s), APIKey: sec(Account(Aliyun, s, "apiKey")), Model: model}
	case Volc:
		v := cloudasr.Volc{Resource: VolcResource(s), AgentPlan: VolcMode(s) == VolcAgent}
		if model == "doubao-seed-asr-2-0" || v.AgentPlan {
			v.Resource = cloudasr.VolcHourly
		}
		if VolcMode(s) == VolcLegacy {
			v.AppID, v.AccessToken = sec(Account(Volc, s, "appId")), sec(Account(Volc, s, "token"))
		} else {
			v.APIKey = sec(Account(Volc, s, "apiKey"))
		}
		return v
	}
	c, _ := Custom(provider, s)
	return asr.Local(c.URL)
}

// LocalKeyAccount is the encrypted local credential store account of the local oMLX key, as it
// has been since keys moved to the encrypted local credential store.
const LocalKeyAccount = "cleanup-api-key"

// CleanupOptions is where text cleanup goes for the default model.
func CleanupOptions(s config.Settings, sec Secrets) (cleanup.Options, error) {
	if ManagedVoice(s) {
		return cleanup.Options{}, errors.New("sub2api voice includes cleanup in the audio task; standalone cleanup is unavailable")
	}
	if err := Check(KindCleanup, s, sec); err != nil {
		return cleanup.Options{}, err
	}
	a := s.CleanupAssign
	switch a.Provider {
	case Local:
		key := sec(LocalKeyAccount)
		if key == "" {
			key = s.CleanupAPIKey // a legacy plaintext key, until it is migrated
		}
		return cleanup.Options{BaseURL: s.CleanupURL, Model: s.CleanupModel, APIKey: key, DisableThinking: true}, nil
	case DeepSeek:
		return cleanup.Options{BaseURL: APIBase(Endpoint(s)), Model: a.Model, APIKey: sec(Account(DeepSeek, s, "apiKey")), DisableDeepSeekThinking: true}, nil
	}
	c, _ := Custom(a.Provider, s)
	if _, err := cleanup.ThinkingParams(c.ThinkingMode, c.ThinkingParams); err != nil {
		return cleanup.Options{}, err
	}
	return cleanup.Options{BaseURL: APIBase(c.URL), Model: a.Model, APIKey: sec(CustomAccount(c.ID)), ThinkingMode: c.ThinkingMode, ThinkingParams: c.ThinkingParams}, nil
}
