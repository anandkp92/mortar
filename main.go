package main

import (
	"context"
	"errors"
	"git.sr.ht/~gabe/mortar/stages"
	"github.com/heptiolabs/healthcheck"
	"github.com/pkg/profile"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	logrus "github.com/sirupsen/logrus"
	"net/http"
	"os"
)

var log = logrus.New()

func init() {
	log.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, ForceColors: true})
	log.SetOutput(os.Stdout)
	log.SetLevel(logrus.DebugLevel)
}

func main() {
	doCPUprofile := false
	if doCPUprofile {
		defer profile.Start(profile.CPUProfile, profile.ProfilePath(".")).Stop()
	}
	doBlockprofile := false
	if doBlockprofile {
		defer profile.Start(profile.BlockProfile, profile.ProfilePath(".")).Stop()
	}

	maincontext, cancel := context.WithCancel(context.Background())

	cfg, err := stages.ReadConfig("mortarconfig.yml")
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("%+v", cfg)

	brickready := false
	health := healthcheck.NewHandler()
	health.AddReadinessCheck("brick", func() error {
		if !brickready {
			return errors.New("Brick not ready")
		}
		return nil
	})
	go http.ListenAndServe("0.0.0.0:8086", health)

	http.Handle("/metrics", promhttp.Handler())
	go func() {
		log.Infof("Prometheus endpoint at %s", cfg.PrometheusAddr)
		if err := http.ListenAndServe(cfg.PrometheusAddr, nil); err != nil {
			log.Fatal(err)
		}
	}()

	frontend_stage_cfg := &stages.ApiFrontendBasicStageConfig{
		StageContext: maincontext,
		ListenAddr:   cfg.ListenAddr,
		AuthConfig:   cfg.Cognito,
		TLSCrtFile:   cfg.TLSCrtFile,
		TLSKeyFile:   cfg.TLSKeyFile,
	}
	frontend_stage, err := stages.NewApiFrontendBasicStage(frontend_stage_cfg)
	if err != nil {
		log.Fatal(err)
	}

	md_stage_cfg := &stages.BrickQueryStageConfig{
		Upstream:          frontend_stage,
		StageContext:      maincontext,
		HodConfigLocation: cfg.HodConfig,
	}

	md_stage, err := stages.NewBrickQueryStage(md_stage_cfg)
	if err != nil {
		log.Fatal(err)
	}
	brickready = true

	ts_stage_cfg := &stages.TimeseriesStageConfig{
		Upstream:     md_stage,
		StageContext: maincontext,
		BTrDBAddress: cfg.BTrDBAddr,
	}
	ts_stage, err := stages.NewTimeseriesQueryStage(ts_stage_cfg)
	if err != nil {
		log.Fatal(err)
	}

	//	ts_stage_cfg := &stages.InfluxDBTimeseriesStageConfig{
	//		Upstream:     md_stage,
	//		StageContext: maincontext,
	//		Address:      cfg.InfluxDBAddr,
	//		Username:     cfg.InfluxDBUser,
	//		Password:     cfg.InfluxDBPass,
	//	}
	//	ts_stage, err := stages.NewInfluxDBTimeseriesQueryStage(ts_stage_cfg)
	//	if err != nil {
	//		log.Fatal(err)
	//	}

	//_ = ts_stage

	var end stages.Stage = ts_stage
	for end != nil {
		log.Println(end)
		end = end.GetUpstream()
	}

	select {}
	cancel()
}
