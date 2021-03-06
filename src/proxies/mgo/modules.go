/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"fmt"
)

type module interface {
	request(string, *mongo_req) error
	config(map[string]interface{}) error
}

var modules map[string]module = map[string]module {}

func addModule(name string, mod module) {
	modules[name] = mod
}

func listModules() {
	for m, _ := range modules {
		fmt.Printf("%s\n", m)
	}
}

func loadModules(config *Config) error {
	for mod, mconf := range config.Modules {
		m, ok := modules[mod]
		if !ok {
			return fmt.Errorf("Error: no %s module\n", mod)
		}

		err := m.config(mconf)
		if err != nil {
			return fmt.Errorf("Error configuring %s: %s\n", mod, err.Error())
		}

		pipelineAdd(m)
	}

	return nil
}
