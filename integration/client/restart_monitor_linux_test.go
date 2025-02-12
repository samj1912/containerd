/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package client

import (
	"context"
	"fmt"
	"syscall"
	"testing"
	"time"

	. "github.com/containerd/containerd"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/oci"
)

// TestRestartMonitor tests restarting containers
// with the restart monitor service plugin
func TestRestartMonitor(t *testing.T) {
	const (
		interval = 10 * time.Second
		epsilon  = 1 * time.Second
		count    = 20
	)
	configTOML := fmt.Sprintf(`
version = 2
[plugins]
  [plugins."io.containerd.internal.v1.restart"]
	  interval = "%s"
`, interval.String())
	client, _, cleanup := newDaemonWithConfig(t, configTOML)
	defer cleanup()

	var (
		ctx, cancel = testContext(t)
		id          = t.Name()
	)
	defer cancel()

	image, err := client.Pull(ctx, testImage, WithPullUnpack)
	if err != nil {
		t.Fatal(err)
	}

	container, err := client.NewContainer(ctx, id,
		WithNewSnapshot(id, image),
		WithNewSpec(
			oci.WithImageConfig(image),
			withProcessArgs("sleep", "infinity"),
		),
		withRestartStatus(Running),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer container.Delete(ctx, WithSnapshotCleanup)

	task, err := container.NewTask(ctx, empty())
	if err != nil {
		t.Fatal(err)
	}
	defer task.Delete(ctx, WithProcessKill)

	if err := task.Start(ctx); err != nil {
		t.Fatal(err)
	}

	if err := task.Kill(ctx, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}

	begin := time.Now()

	expected := begin.Add(interval).Add(epsilon)
	deadline := begin.Add(interval).Add(epsilon * count)
	for {
		status, err := task.Status(ctx)
		now := time.Now()
		if err != nil {
			// ErrNotFound is expected here, because the restart monitor
			// temporarily removes the task before restarting.
			t.Logf("%v: err=%v", now, err)
		} else {
			t.Logf("%v: status=%q", now, status)

			if status.Status == Running {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Logf("%v: the task was not restarted", now)
			return
		}
		time.Sleep(epsilon)
	}

	now := time.Now()
	if now.After(expected) {
		t.Fatalf("%v: the task was restarted, but it must be before %v", now, expected)
	}
	t.Logf("%v: the task was restarted before %v", now, expected)
}

// withRestartStatus is a copy of "github.com/containerd/containerd/runtime/restart".WithStatus.
// This copy is needed because `go test` refuses circular imports.
func withRestartStatus(status ProcessStatus) func(context.Context, *Client, *containers.Container) error {
	return func(_ context.Context, _ *Client, c *containers.Container) error {
		if c.Labels == nil {
			c.Labels = make(map[string]string)
		}
		c.Labels["containerd.io/restart.status"] = string(status)
		return nil
	}
}
