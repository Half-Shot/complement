package docker

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	client "github.com/docker/docker/client"
)

// A volume is a mounted directory on the homeserver container
type Volume interface {
	// Return the mount point
	Mount() mount.Mount
	// Prepare the mount point. `contextStr` is unique to this blueprint+homeserver for homeserver
	// specific mounts e.g appservices.
	Prepare(ctx context.Context, docker *client.Client, contextStr string) error
}

type VolumeCA struct {
	source string
}

// Prepare the Certificate Authority volume. This is independent of the homeserver calling Prepare
// hence the contextual string is unused.
func (v *VolumeCA) Prepare(ctx context.Context, docker *client.Client, x string) error {
	// TODO: wrap in a lockfile
	if os.Getenv("CI") == "true" {
		// When in CI, Complement itself is a container with the CA volume mounted at /ca.
		// We need to mount this volume to all homeserver containers to synchronize the CA cert.
		// This is needed to establish trust among all containers.

		// Get volume mounted at /ca. First we get the container ID
		// /proc/1/cpuset should be /docker/<containerID>
		cpuset, err := ioutil.ReadFile("/proc/1/cpuset")
		if err != nil {
			return err
		}
		if !strings.Contains(string(cpuset), "docker") {
			return fmt.Errorf("could not identify container ID using /proc/1/cpuset - cpuset=%s", string(cpuset))
		}
		cpusetList := strings.Split(strings.TrimSpace(string(cpuset)), "/")
		containerID := cpusetList[len(cpusetList)-1]
		container, err := docker.ContainerInspect(ctx, containerID)
		if err != nil {
			return err
		}
		// Get the volume that matches the destination in our complement container
		var volumeName string
		for i := range container.Mounts {
			if container.Mounts[i].Destination == "/ca" {
				volumeName = container.Mounts[i].Name
			}
		}
		if volumeName == "" {
			// We did not find a volume. This container might be created without a volume,
			// or CI=true is passed but we are not running in a container.
			return fmt.Errorf("CI=true but there is no /ca mounted to Complement's container")
		}
		v.source = volumeName
	} else {
		// When not in CI, our CA cert is placed in the current working dir.
		// We bind mount this directory to all homeserver containers.
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		caCertificateDirHost := path.Join(cwd, "ca")
		if _, err := os.Stat(caCertificateDirHost); os.IsNotExist(err) {
			err = os.Mkdir(caCertificateDirHost, 0770)
			if err != nil {
				return err
			}
		}
		v.source = path.Join(cwd, "ca")
	}
	return nil
}

func (v *VolumeCA) Mount() mount.Mount {
	return mount.Mount{
		Type:   mount.TypeBind,
		Source: v.source,
		Target: "/ca",
	}
}

type VolumeAppService struct {
	source string
}

func (v *VolumeAppService) Prepare(ctx context.Context, docker *client.Client, contextStr string) error {
	asVolume, err := docker.VolumeCreate(context.Background(), volume.VolumesCreateBody{
		Name: "appservices",
	})
	if err != nil {
		return err
	}
	v.source = asVolume.Name
	return nil
}

func (v *VolumeAppService) Mount() mount.Mount {
	return mount.Mount{
		Type:   mount.TypeVolume,
		Source: v.source,
		Target: "/appservices",
	}
}
