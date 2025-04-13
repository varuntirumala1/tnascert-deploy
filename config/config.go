/*
 * Copyright (C) 2025 by John J. Rushford jrushford@apache.org
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package config

import (
	"fmt"
	"gopkg.in/ini.v1"
)

const (
	WS                     = "ws"
	WSS                    = "wss"
	Config_file            = "tnas-cert.ini"
	Default_base_cert_name = "tnas-cert-deploy"
	Default_section        = "default"
	Default_port           = 443
	Default_protocol       = WSS
)

type Config struct {
	Api_key               string `ini:"api_key"`               // TrueNAS 64 byte API Key
	Cert_basename         string `ini:"cert_basename"`         // basename for cert naming in TrueNAS
	Connect_host          string `ini:"connect_host"`          // TrueNAS hostname
	Delete_old_certs      bool   `ini:"delete_old_certs"`      // whether to remove old certificates
	Fullchain_path        string `ini:"full_chain_path"`       // path to full_chain.pem
	Port                  uint64 `ini:"port"`                  // TrueNAS API endpoint port
	Protocol              string `ini:"protocol"`              // websocket protocol 'ws' or 'wss' 'wss' is default
	Private_key_path      string `ini:"private_key_path"`      // path to private_key.pem
	TLS_skip_verify       bool   `ini:"tls_skip_verify"`       // strict SSL cert verification of the endpoint
	Add_as_ui_certificate bool   `ini:"add_as_ui_certificate"` // Install as the active UI certificate if true
}

func New(config_file string, section string) (*Config, error) {
	c := Config{}

	// load the config file
	cfg, err := ini.Load(config_file)
	if err != nil {
		return nil, err
	}

	// lookup the config section
	_, err = cfg.GetSection(section)
	if err != nil {
		return nil, err
	}

	// map the config
	err = cfg.Section(section).MapTo(&c)
	if err != nil {
		return nil, err
	}

	err = c.checkConfig()
	if err != nil {
		return nil, err
	}

	return &c, nil
}

func (c *Config) checkConfig() error {
	if len(c.Api_key) < 66 {
		return fmt.Errorf("invalid or empty api_key")
	}
	// if not the cert_basename is not defined use the default
	if c.Cert_basename == "" {
		c.Cert_basename = Default_base_cert_name
	}
	if c.Connect_host == "" {
		return fmt.Errorf("connect_host is not defined")
	}
	if c.Fullchain_path == "" {
		return fmt.Errorf("fullchain_path is not defined")
	}
	// if port is not defined, use the default
	if c.Port == 0 {
		c.Port = Default_port
	}
	// if the protocol is not defined, use the default
	if len(c.Protocol) == 0 {
		c.Protocol = Default_protocol
	} else {
		if c.Protocol != WS && c.Protocol != WSS {
			return fmt.Errorf("invalid protocol")
		}
	}
	if c.Private_key_path == "" {
		return fmt.Errorf("private_key_path is not defined")
	}

	return nil
}
