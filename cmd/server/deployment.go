package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/moby/moby/client"
	"gostream3d/internal/config"
	"gostream3d/internal/orchestrator"
)

func checkDeployment() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Runtime == "docker" {
		cli, err := orchestrator.NewDockerClientFromEnv()
		if err != nil {
			return err
		}
		defer cli.Close()
		ports, err := orchestrator.NewPortAllocator(cfg.HTTPPorts, cfg.CDPPorts, cfg.UDPPorts, cfg.UDPPerWorker)
		if err != nil {
			return err
		}
		if _, err := orchestrator.NewDockerManager(cli, ports, cfg.Docker); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
			return fmt.Errorf("Docker unavailable: %w; check Docker service and socket permissions", err)
		}
		image := cfg.Docker.Image
		if image == "" {
			image = "gostream3d/neko/chromium:3.1.5"
		}
		if _, err := cli.ImageInspect(ctx, image); err != nil {
			return fmt.Errorf("worker image %q is unavailable: %w; run install-linux.sh or load the offline image", image, err)
		}
		fmt.Println("Docker and CPU worker image:", image)
		return nil
	}
	if cfg.Runtime == "native" {
		var missing error
		for _, path := range []string{cfg.Native.NekoBinary, cfg.Native.ChromiumBinary, cfg.Native.XorgBinary, cfg.Native.PulseBinary} {
			if _, err := exec.LookPath(path); err != nil {
				missing = errors.Join(missing, fmt.Errorf("missing executable %q: %w", path, err))
			}
		}
		return missing
	}
	return nil
}
