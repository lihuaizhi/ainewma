package providers

type TCMProviderConfig struct {
	DefaultFangjiProvider string
	DefaultYaocaiProvider string
	LocalDataDir          string
	APIBaseURL            string
	APIKey                string
	TimeoutMS             int
}

type TCMProviderFactory struct{}

func NewTCMProviderFactory() *TCMProviderFactory {
	return &TCMProviderFactory{}
}

func (f *TCMProviderFactory) CreateLocal(reg *ProviderRegistry, cfg TCMProviderConfig) *ProviderRegistry {
	if reg == nil {
		reg = NewProviderRegistry()
	}
	localFangji := NewLocalFangjiProvider("local", "Local TCM Formulas", true, cfg.LocalDataDir)
	localYaocai := NewLocalYaocaiProvider("local", "Local TCM Herbs", true, cfg.LocalDataDir)
	reg.RegisterFangji(localFangji)
	reg.RegisterYaocai(localYaocai)
	if cfg.DefaultFangjiProvider != "" {
		reg.SetDefaultFangji(cfg.DefaultFangjiProvider)
	}
	if cfg.DefaultYaocaiProvider != "" {
		reg.SetDefaultYaocai(cfg.DefaultYaocaiProvider)
	}
	return reg
}
