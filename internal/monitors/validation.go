package monitors

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/signalfx/signalfx-agent/internal/core/config"
	"github.com/signalfx/signalfx-agent/internal/core/config/validation"
	"github.com/signalfx/signalfx-agent/internal/core/services"
)

// Used to validate configuration that is common to all monitors up front
func validateConfig(monConfig config.MonitorCustomConfig) error {
	conf := monConfig.MonitorConfigCore()

	if _, ok := MonitorFactories[conf.Type]; !ok {
		return errors.New("monitor type not recognized")
	}

	if conf.IntervalSeconds <= 0 {
		return fmt.Errorf("invalid intervalSeconds provided: %d", conf.IntervalSeconds)
	}

	takesEndpoints := configAcceptsEndpoints(monConfig)
	if !takesEndpoints && conf.DiscoveryRule != "" {
		return fmt.Errorf("monitor %s does not support discovery but has a discovery rule", conf.Type)
	}

	// Validate discovery rules
	if conf.DiscoveryRule != "" {
		err := services.ValidateDiscoveryRule(conf.DiscoveryRule)
		if err != nil {
			return errors.New("discovery rule is invalid: " + err.Error())
		}
	}

	if len(conf.ConfigEndpointMappings) > 0 && len(conf.DiscoveryRule) == 0 {
		return errors.New("configEndpointMappings is not useful without a discovery rule")
	}

	if err := validation.ValidateStruct(monConfig); err != nil {
		return err
	}

	return validation.ValidateCustomConfig(monConfig)
}

// Configuration with discovery rules is a bit tricky to validate since in its
// given form, it will never validate since there is no host/port.  But we need
// a way to give upfront feedback if there are other validation issues with the
// config since otherwise the user has to wait until the endpoint has been
// discovered and the monitor tries to initialize to see validation errors.
func validateConfigWithDiscoveryRule(monConfig config.MonitorCustomConfig) error {
}

func configAcceptsEndpoints(monConfig config.MonitorCustomConfig) bool {
	confVal := reflect.Indirect(reflect.ValueOf(monConfig))
	coreConfField, ok := confVal.Type().FieldByName("MonitorConfig")
	if !ok {
		return false
	}
	return coreConfField.Tag.Get("acceptsEndpoints") == "true"
}

func isConfigUnique(conf *config.MonitorConfig, otherConfs []config.MonitorConfig) bool {
	for _, c := range otherConfs {
		if c.MonitorConfigCore().Equals(conf) {
			return true
		}
	}
	return false
}
