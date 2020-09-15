// nolint: gosec,goconst
package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
)

// Setup sets up testnet configuration in a directory.
func Setup(testnet *Testnet, dir string) error {
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	compose, err := MakeDockerCompose(testnet)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(dir, "docker-compose.yml"), compose, 0644); err != nil {
		return err
	}

	genesis, err := MakeGenesis(testnet)
	if err != nil {
		return err
	}
	for _, node := range testnet.Nodes {
		nodeDir := filepath.Join(dir, node.Name)
		cfg, err := MakeConfig(testnet, node)
		if err != nil {
			return err
		}
		appCfg, err := MakeAppConfig(testnet, node)
		if err != nil {
			return err
		}
		pv := privval.NewFilePV(node.Key,
			filepath.Join(nodeDir, "config", "priv_validator_key.json"),
			filepath.Join(nodeDir, "data", "priv_validator_state.json"),
		)

		if err := os.MkdirAll(nodeDir, 0755); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(nodeDir, "config"), 0755); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(nodeDir, "data"), 0755); err != nil {
			return err
		}
		if err := genesis.SaveAs(filepath.Join(nodeDir, "config", "genesis.json")); err != nil {
			return err
		}
		config.WriteConfigFile(filepath.Join(nodeDir, "config", "config.toml"), cfg) // panics
		if err := ioutil.WriteFile(filepath.Join(nodeDir, "config", "app.toml"), appCfg, 0644); err != nil {
			return err
		}
		if err := genesis.SaveAs(filepath.Join(nodeDir, "config", "genesis.json")); err != nil {
			return err
		}
		if err := MakeNodeKey(node).SaveAs(filepath.Join(nodeDir, "config", "node_key.json")); err != nil {
			return err
		}
		pv.Save() // panics
	}

	return nil
}

// MakeDockerCompose generates a Docker Compose config for a testnet.
// Must use version 2 Docker Compose format, to support IPv6.
func MakeDockerCompose(testnet *Testnet) ([]byte, error) {
	tmpl, err := template.New("docker-compose").Parse(`version: '2.4'

networks:
  {{ .Name }}:
    driver: bridge
{{- if .IsIPv6 }}
    enable_ipv6: true
{{- end }}
    ipam:
      driver: default
      config:
      - subnet: {{ .IP }}

services:
{{- range .Nodes }}
  {{ .Name }}:
    container_name: {{ .Name }}
    image: tendermint/e2e-node
    init: true
    ports:
    - 26656
    - {{ if .ProxyPort }}{{ .ProxyPort }}:{{ end }}26657
    volumes:
    - ./{{ .Name }}:/tendermint
    networks:
      {{ $.Name }}:
        ipv{{ if $.IsIPv6 }}6{{ else }}4{{ end}}_address: {{ .IP }}

{{end}}`)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, testnet)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MakeGenesis generates a genesis document.
func MakeGenesis(testnet *Testnet) (types.GenesisDoc, error) {
	genesis := types.GenesisDoc{
		GenesisTime:     time.Now(),
		ChainID:         testnet.Name,
		ConsensusParams: types.DefaultConsensusParams(),
		InitialHeight:   int64(testnet.InitialHeight),
	}
	for _, node := range testnet.Nodes {
		genesis.Validators = append(genesis.Validators, types.GenesisValidator{
			Name:    node.Name,
			Address: node.Key.PubKey().Address(),
			PubKey:  node.Key.PubKey(),
			Power:   100,
		})
	}
	err := genesis.ValidateAndComplete()
	return genesis, err
}

// MakeConfig generates a Tendermint config for a node.
func MakeConfig(testnet *Testnet, node *Node) (*config.Config, error) {
	cfg := config.DefaultConfig()
	cfg.Moniker = node.Name
	cfg.ProxyApp = "tcp://127.0.0.1:30000"
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"
	cfg.DBBackend = node.Database

	switch node.ABCIProtocol {
	case "unix":
		cfg.ProxyApp = "unix:///var/run/app.sock"
	case "tcp":
		cfg.ProxyApp = "tcp://127.0.0.1:30000"
	case "grpc":
		cfg.ProxyApp = "tcp://127.0.0.1:30000"
		cfg.ABCI = "grpc"
	default:
		return nil, fmt.Errorf("unexpected ABCI protocol setting %q", node.ABCIProtocol)
	}

	if node.FastSync == "" {
		cfg.FastSyncMode = false
	} else {
		cfg.FastSync.Version = node.FastSync
	}

	for _, peer := range testnet.Nodes {
		if peer.Name == node.Name {
			continue
		}
		if cfg.P2P.PersistentPeers != "" {
			cfg.P2P.PersistentPeers += ","
		}
		if testnet.IsIPv6() {
			cfg.P2P.PersistentPeers += fmt.Sprintf("%x@[%v]:%v", peer.Key.PubKey().Address().Bytes(), peer.IP, 26656)
		} else {
			cfg.P2P.PersistentPeers += fmt.Sprintf("%x@%v:%v", peer.Key.PubKey().Address().Bytes(), peer.IP, 26656)
		}
	}
	return cfg, nil
}

// MakeAppConfig generates an ABCI application config for a node.
func MakeAppConfig(testnet *Testnet, node *Node) ([]byte, error) {
	cfg := map[string]interface{}{}
	switch node.ABCIProtocol {
	case "unix":
		cfg["listen"] = "unix:///var/run/app.sock"
		cfg["grpc"] = false
	case "tcp":
		cfg["listen"] = "tcp://127.0.0.1:30000"
		cfg["grpc"] = false
	case "grpc":
		cfg["listen"] = "tcp://127.0.0.1:30000"
		cfg["grpc"] = true
	default:
		return nil, fmt.Errorf("unexpected ABCI protocol setting %q", node.ABCIProtocol)
	}

	var buf bytes.Buffer
	err := toml.NewEncoder(&buf).Encode(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to generate app config: %w", err)
	}
	return buf.Bytes(), nil
}

// MakeNodeKey generates a node key.
func MakeNodeKey(node *Node) *p2p.NodeKey {
	return &p2p.NodeKey{PrivKey: node.Key}
}