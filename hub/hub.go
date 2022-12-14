package hub

import (
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"

	"github.com/Dreamacro/clash/config"
	"github.com/Dreamacro/clash/hub/executor"
	"github.com/Dreamacro/clash/hub/route"
	"github.com/Dreamacro/clash/log"
)

type Option func(*config.Config)

func WithExternalUI(externalUI string) Option {
	return func(cfg *config.Config) {
		cfg.General.ExternalUI = externalUI
	}
}

func WithExternalController(externalController string) Option {
	return func(cfg *config.Config) {
		cfg.General.ExternalController = externalController
	}
}

func WithSecret(secret string) Option {
	return func(cfg *config.Config) {
		cfg.General.Secret = secret
	}
}

func enablePProfSvr(port int) {
	if port == 0 {
		return
	}
	// https://golang.org/pkg/net/http/pprof/
	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Errorln("[pprof] unexpected error occurred in pprof server: %s", err)
			}
		}()

		listener, err := net.Listen("tcp", ":"+fmt.Sprint(port))
		if err != nil {
			log.Errorln("[pprof] net.Listen error: %s", err)
			return
		}

		log.Infoln("[pprof] net.Listen port: %d", listener.Addr().(*net.TCPAddr).Port)
		err = http.Serve(listener, nil)
		if err != nil {
			log.Errorln("[pprof] http.ListenAndServe error: %s", err)
		}
	}()
}

// Parse call at the beginning of clash
func Parse(options ...Option) error {
	cfg, err := executor.Parse()
	if err != nil {
		return err
	}

	enablePProfSvr(cfg.General.PprofPort)

	for _, option := range options {
		option(cfg)
	}

	if cfg.General.ExternalUI != "" {
		route.SetUIPath(cfg.General.ExternalUI)
	}

	if cfg.General.ExternalController != "" {
		go route.Start(cfg.General.ExternalController, cfg.General.Secret)
	}

	executor.ApplyConfig(cfg, true)
	return nil
}
