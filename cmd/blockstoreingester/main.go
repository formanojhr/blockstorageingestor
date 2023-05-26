package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v2"
	"io"
	"os"
	"strings"

	"github.com/cortexproject/cortex/pkg/cortex"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

const (
	configFileOption = "config.file"
	configExpandENV  = "config.expand-env"
)

var testMode = false

// configHash exposes information about the loaded config
var configHash *prometheus.GaugeVec = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "cortex_config_hash",
		Help: "Hash of the currently active config file.",
	},
	[]string{"sha256"},
)

// TODO initialize the block storage ingester
func main() {
	var (
		cfg cortex.Config
		//eventSampleRate      int
		//ballastBytes         int
		//mutexProfileFraction int
		//blockProfileRate     int
		//printVersion         bool
		//printModules         bool
	)

	configFile, expandENV := parseConfigFileParameter(os.Args[1:])

	// This sets default values from flags to the config.
	// It needs to be called before parsing the config file!
	flagext.RegisterFlags(&cfg)

	if configFile != "" {
		if err := LoadConfig(configFile, expandENV, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error loading config from %s: %v\n", configFile, err)
			if testMode {
				return
			}
			os.Exit(1)
		}
	}

	// Ignore -config.file and -config.expand-env here, since it was already parsed, but it's still present on command line.
	flagext.IgnoredFlag(flag.CommandLine, configFileOption, "Configuration file to load.")
	_ = flag.CommandLine.Bool(configExpandENV, false, "Expands ${var} or $var in config according to the values of the environment variables.")

}

// Parse -config.file and -config.expand-env option via separate flag set, to avoid polluting default one and calling flag.Parse on it twice.
func parseConfigFileParameter(args []string) (configFile string, expandEnv bool) {
	// ignore errors and any output here. Any flag errors will be reported by main flag.Parse() call.
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	// usage not used in these functions.
	fs.StringVar(&configFile, configFileOption, "", "")
	fs.BoolVar(&expandEnv, configExpandENV, false, "")

	// Try to find -config.file and -config.expand-env option in the flags. As Parsing stops on the first error, eg. unknown flag, we simply
	// try remaining parameters until we find config flag, or there are no params left.
	// (ContinueOnError just means that flag.Parse doesn't call panic or os.Exit, but it returns error, which we ignore)
	for len(args) > 0 {
		_ = fs.Parse(args)
		args = args[1:]
	}

	return
}

// LoadConfig read YAML-formatted config from filename into cfg.
func LoadConfig(filename string, expandENV bool, cfg *cortex.Config) error {
	buf, err := os.ReadFile(filename)
	if err != nil {
		return errors.Wrap(err, "Error reading config file")
	}

	// create a sha256 hash of the config before expansion and expose it via
	// the config_info metric
	hash := sha256.Sum256(buf)
	configHash.Reset()
	configHash.WithLabelValues(fmt.Sprintf("%x", hash)).Set(1)

	if expandENV {
		buf = expandEnv(buf)
	}

	err = yaml.UnmarshalStrict(buf, cfg)
	if err != nil {
		return errors.Wrap(err, "Error parsing config file")
	}

	return nil
}

// expandEnv replaces ${var} or $var in config according to the values of the current environment variables.
// The replacement is case-sensitive. References to undefined variables are replaced by the empty string.
// A default value can be given by using the form ${var:default value}.
func expandEnv(config []byte) []byte {
	return []byte(os.Expand(string(config), func(key string) string {
		keyAndDefault := strings.SplitN(key, ":", 2)
		key = keyAndDefault[0]

		v := os.Getenv(key)
		if v == "" && len(keyAndDefault) == 2 {
			v = keyAndDefault[1] // Set value to the default.
		}
		return v
	}))
}
