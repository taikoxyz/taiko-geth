package mevcommit

import (
	"fmt"
	"net/http"

	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
)

type Service struct {
	srv       *http.Server
	mevcommit IMevCommit
}

func (s *Service) Start() error {
	if s.srv != nil {
		log.Info("Service started")
		go s.srv.ListenAndServe()
	}

	s.mevcommit.Start()
	return nil
}

func (s *Service) Stop() error {
	if s.srv != nil {
		s.srv.Close()
	}

	s.mevcommit.Stop()
	return nil
}

func NewService(mevcommit IMevCommit) *Service {
	var srv *http.Server
	//TODO create server

	return &Service{
		srv:       srv,
		mevcommit: mevcommit,
	}
}

func Register(stack *node.Node, backend *eth.Ethereum, cfg *Config) error {
	ethereumService := NewEthereumService(backend)
	providerService := NewProvider(backend, cfg.MevCommitProviderEndpoint)

	mevCommitArgs := MevCommitArgs{
		eth:             ethereumService,
		provider:        providerService,
		providerEnabled: cfg.MevCommitProviderEnabled,
	}

	mevCommitBackend, err := NewMevCommit(mevCommitArgs)
	if err != nil {
		return fmt.Errorf("failed to create mevcommit backend: %w", err)
	}

	mevcommitService := NewService(mevCommitBackend)

	stack.RegisterAPIs([]rpc.API{
		{
			Namespace:     "mevcommit",
			Version:       "v0.3.0-rc2",
			Service:       mevcommitService,
			Public:        true,
			Authenticated: true,
		},
	})

	stack.RegisterLifecycle(mevcommitService)

	return nil
}
