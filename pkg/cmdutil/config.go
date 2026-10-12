// Package cmdutil holds helpers shared by the binaries: configuration loading.
package cmdutil

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	flag "github.com/spf13/pflag"
)

// LoadConfig fills out, in this order: the embedded defaults, the --config file
// when the flag is set, and environment variables. Environment variables carry
// no prefix. A double underscore nests keys: POSTGRES__HOST sets postgres.host.
// Variables without a double underscore are ignored. A comma-separated value fills
// a list-typed key, so ENGINE__RETRY__DELAYS=5s,10s is a list of two delays; a
// string key keeps its commas.
func LoadConfig(name string, defaults []byte, out any) error {
	k := koanf.New(".")

	if err := k.Load(rawbytes.Provider(defaults), yaml.Parser()); err != nil {
		return fmt.Errorf("load default config: %w", err)
	}

	if err := loadFlags(k, name); err != nil {
		return fmt.Errorf("load flags: %w", err)
	}

	if configPath := k.String("config"); configPath != "" {
		if err := k.Load(file.Provider(configPath), yaml.Parser()); err != nil {
			return fmt.Errorf("load config file: %w", err)
		}
	}

	if err := k.Load(env.Provider(".", env.Opt{
		TransformFunc: func(key, value string) (string, any) {
			if !strings.Contains(key, "__") {
				return "", nil
			}

			return strings.ToLower(strings.ReplaceAll(key, "__", ".")), value
		},
	}), nil); err != nil {
		return fmt.Errorf("load env: %w", err)
	}

	conf := koanf.UnmarshalConf{Tag: "yaml", DecoderConfig: decoderConfig()}
	if err := k.UnmarshalWithConf("", out, conf); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}

	return nil
}

// decoderConfig is the decoder of the configuration. Its decode hooks are koanf's defaults,
// which read durations and text-unmarshaled values, plus one that splits a string at each
// comma when the target is a list. koanf's text hook is an unexported copy of
// mapstructure.TextUnmarshallerHookFunc, and the two agree for the key types of the server.
// The split hook acts on list targets only, so a string key keeps its commas.
// WeaklyTypedInput is koanf's default too. StringToSliceHookFunc is not used, because it
// splits only into a []string and not into a list of durations.
func decoderConfig() *mapstructure.DecoderConfig {
	return &mapstructure.DecoderConfig{
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.TextUnmarshallerHookFunc(),
			mapstructure.StringToWeakSliceHookFunc(","),
		),
		WeaklyTypedInput: true,
	}
}

func loadFlags(k *koanf.Koanf, name string) error {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.Usage = func() {
		fmt.Println(f.FlagUsages())
		os.Exit(0)
	}

	f.String("config", "", "path to config file, if not provided, the default configuration will be used")

	if err := f.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if err := k.Load(posflag.Provider(f, ".", k), nil); err != nil {
		return fmt.Errorf("load flags: %w", err)
	}

	return nil
}
