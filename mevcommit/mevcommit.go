package mevcommit

import "time"

type IMevCommit interface {
	Start() error
	Stop() error
}

type MevCommit struct {
	eth             IEthereumService
	provider        IProviderService
	providerEnabled bool
	stop            chan struct{}
}

type MevCommitArgs struct {
	eth             IEthereumService
	provider        IProviderService
	providerEnabled bool
}

func NewMevCommit(args MevCommitArgs) (*MevCommit, error) {
	return &MevCommit{
		eth:             args.eth,
		provider:        args.provider,
		providerEnabled: args.providerEnabled,

		stop: make(chan struct{}, 1),
	}, nil
}

func (m *MevCommit) Start() error {
	runProvider := func() {
		if !m.providerEnabled {
			return
		}
		for {
			if m.eth.Synced() {
				m.provider.Run()
				time.Sleep(time.Minute)
			}
			time.Sleep(time.Minute)
		}
	}

	go runProvider()
	return nil
}

func (m *MevCommit) Stop() error {
	close(m.stop)
	return nil
}
